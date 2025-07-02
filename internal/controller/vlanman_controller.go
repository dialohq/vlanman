package controller

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"github.com/alecthomas/repr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	errs "dialo.ai/vlanman/pkg/errors"
	"dialo.ai/vlanman/pkg/locker"
	u "dialo.ai/vlanman/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Envs holds environment configuration values for the IPSec controller
type Envs struct {
	NamespaceName            string
	VlanManagerImage         string
	VlanManagerPullPolicy    string
	IsTest                   bool
	WaitForPodTimeoutSeconds int64
	IsMonitoringEnabled      bool
	MonitoringScrapeInterval string
	MonitoringReleaseName    string
	TTL                      *int32
	InterfacePodImage        string
	InterfacePodPullPolicy   string
	WorkerInitImage          string
	WorkerInitPullPolicy     string
	ServiceAccountName       string
}

type VlanmanReconciler struct {
	Client client.Client
	Scheme *k8sRuntime.Scheme
	Env    Envs
	Config *rest.Config
}

type ReconcileError struct {
	Action reflect.Type
	Err    error
}

func (e *ReconcileError) Error() string {
	return fmt.Sprintf("Error doing %s: %s", e.Action, e.Err)
}

func (e *ReconcileError) Unwrap() error {
	return e.Err
}

type ReconcileErrorList struct {
	Errs []*ReconcileError
}

func (e *ReconcileErrorList) Error() string {
	errStr := []string{}
	msg := fmt.Sprintf("%d Unrecoverable errors encountered: ", len(e.Errs))
	for _, e := range e.Errs {
		errStr = append(errStr, (*e).Error())
	}
	msg += strings.Join(errStr, " |+| ")
	return msg
}

func (r *VlanmanReconciler) reconcilePod(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	pod := corev1.Pod{}
	err := r.Client.Get(ctx, req.NamespacedName, &pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			rq, err := r.updateAllNetworksStatus(ctx)
			if err != nil || rq == false {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, errs.NewClientRequestError("Get pod in reconcilePod", err)
	}
	if pod.Status.PodIP == "" {
		log.Info("Pod doesnt have IP assigned yet, requeing", "pod", req.Name)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	rq, err := r.updateAllNetworksStatus(ctx)
	if err != nil || rq == false {
		return ctrl.Result{}, err
	}
	if rq {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *VlanmanReconciler) getCurrentState(ctx context.Context) ([]ManagerSet, error) {
	managers := appsv1.DaemonSetList{}
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement(vlanmanv1.ManagerSetLabelKey, "exists", nil)
	if err != nil {
		return nil, &errs.InternalError{Context: fmt.Sprintf("Error creating a label selector requirement in createDesiredState: %s", err.Error())}
	}
	selector = selector.Add(*requirement)
	opts := client.ListOptions{
		LabelSelector: selector,
	}
	err = r.Client.List(ctx, &managers, &opts)
	if err != nil {
		return nil, &errs.ClientRequestError{
			Action: "List pods with field selector",
			Err:    err,
		}
	}
	// TODO: check if interfaces are created
	mgrs := []ManagerSet{}
	for _, m := range managers.Items {
		mgrs = append(mgrs, managerFromSet(m))
	}
	return mgrs, nil
}

func (r *VlanmanReconciler) createDesiredState(networks []vlanmanv1.VlanNetwork) []ManagerSet {
	mgrs := []ManagerSet{}
	for _, net := range networks {
		mgrs = append(mgrs, createDesiredManagerSet(net))
	}
	return mgrs
}

func (r *VlanmanReconciler) diffManagers(desired, current ManagerSet) []Action {
	eq := true
	eq = eq && desired.OwnerNetworkName == current.OwnerNetworkName
	eq = eq && desired.VlanID == current.VlanID
	if !eq {
		return []Action{&DeleteManagerAction{current}, &CreateManagerAction{desired}}
	}
	if len(desired.ExcludedNodes) != len(current.ExcludedNodes) {
		return []Action{&DeleteManagerAction{current}, &CreateManagerAction{desired}}
	}
	slices.Sort(desired.ExcludedNodes)
	slices.Sort(current.ExcludedNodes)
	for idx, n := range desired.ExcludedNodes {
		if n != current.ExcludedNodes[idx] {
			return []Action{&DeleteManagerAction{current}, &CreateManagerAction{desired}}
		}
	}
	return []Action{}
}

func (r *VlanmanReconciler) diffStates(desired, current []ManagerSet) []Action {
	acts := []Action{}

	// sort for searching
	slices.SortFunc(desired, managerCmp)
	slices.SortFunc(current, managerCmp)

	for _, desiredMgr := range desired {
		idx, found := slices.BinarySearchFunc(current, desiredMgr, managerCmp)
		if !found {
			acts = append(acts, &CreateManagerAction{Manager: desiredMgr})
			continue
		}
		acts = append(acts, r.diffManagers(desiredMgr, current[idx])...)
	}

	for _, currentMgr := range current {
		_, found := slices.BinarySearchFunc(desired, currentMgr, managerCmp)
		if !found {
			acts = append(acts, &DeleteManagerAction{Manager: currentMgr})
		}
	}
	return acts
}

func (r *VlanmanReconciler) reconcileNetwork(ctx context.Context) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	errList := []*ReconcileError{}
	rq, err := r.updateAllNetworksStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	networkList := &vlanmanv1.VlanNetworkList{}
	err = r.Client.List(ctx, networkList)
	if err != nil {
		return ctrl.Result{}, &errs.ClientRequestError{
			Action: "List VlanNetworks",
			Err:    err,
		}
	}

	desired := r.createDesiredState(networkList.Items)

	current, err := r.getCurrentState(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	actions := r.diffStates(desired, current)

	if len(actions) == 0 {
		log.Info("No actions to take")
		if rq {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	} else {
		for _, action := range actions {
			log.Info("Doing action", "type", reflect.TypeOf(action))
			err := action.Do(ctx, r)
			if err != nil {
				recErr := &ReconcileError{Action: reflect.TypeOf(action), Err: err}
				errList = append(errList, recErr)
				log.Error(recErr, "Error reconciling")
			}
		}
	}

	if len(errList) != 0 {
		return ctrl.Result{}, &ReconcileErrorList{
			Errs: errList,
		}
	}

	if rq {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func extractVlan(pod corev1.Pod) (string, bool) {
	subnet := ""
	ip := ""
	for _, cont := range pod.Spec.InitContainers {
		for _, e := range cont.Env {
			if e.Name == "MACVLAN_IP" {
				ip = e.Value
			}
			if e.Name == "MACVLAN_SUBNET" {
				subnet = e.Value
			}
		}
	}
	fullIp := ip
	if subnet != "" {
		fullIp += "/" + subnet
	}
	return fullIp, ip != ""
}

func (r *VlanmanReconciler) updateNetworkStatus(ctx context.Context, network vlanmanv1.VlanNetwork) (bool, error) {
	pods := corev1.PodList{}
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement(vlanmanv1.WorkerPodLabelKey, "==", []string{network.Name})
	if err != nil || requirement == nil {
		return true, &errs.InternalError{
			Context: "Couldn't create a requirement for a label selector in updateNetworkStatus: " + err.Error(),
		}
	}
	selector = selector.Add(*requirement)
	opts := client.ListOptions{
		LabelSelector: selector,
	}
	err = r.Client.List(ctx, &pods, &opts)
	if err != nil {
		return true, errs.NewClientRequestError(
			"List Pods in updateNetworkStatus",
			err,
		)
	}
	poolNames := slices.Collect(maps.Keys(network.Spec.Pools))
	network.Status = u.PopulateStatus(network.Status, poolNames...)
	initialPools := maps.Clone(network.Spec.Pools)
	pending := maps.Clone(network.Status.PendingIPs)

	requeue := false
	for _, pod := range pods.Items {
		fmt.Println("Checking pod ", pod.Name)
		podPool, ok := pod.Annotations[vlanmanv1.PodVlanmanIPPoolAnnotation]
		if !ok {
			return requeue, &errs.UnrecoverableError{
				Context: "Pod should have macvlan ip but doesn't",
				Err:     errs.ErrNilUnrecoverable,
			}
		}
		podIP, found := extractVlan(pod)
		fmt.Println("Pod has ip", podIP)
		if !found {
			continue
		}

		initialPools[podPool] = slices.DeleteFunc(initialPools[podPool], func(ip string) bool {
			fmt.Println("in initial")
			if ip != podIP {
				fmt.Println(ip, "doesnt equal", podIP)
			} else {
				fmt.Println(ip, "equals", podIP)
			}
			return ip == podIP
		})

		pending[podPool] = slices.DeleteFunc(pending[podPool], func(ip string) bool {
			fmt.Println("in pending")
			if ip != podIP {
				fmt.Println(ip, "doesnt equal", podIP)
			} else {
				fmt.Println(ip, "equals", podIP)
			}
			return ip == podIP
		})
	}
	repr.Println(initialPools)
	network.Status.PendingIPs = pending
	network.Status.FreeIPs = initialPools

	repr.Println(network.Status)
	err = r.Client.Status().Update(ctx, &network)
	if err != nil {
		return requeue, errs.NewClientRequestError(fmt.Sprintf("Update status for network '%s'", network.Name), err)
	}

	return requeue, nil
}

func (r *VlanmanReconciler) updateAllNetworksStatus(ctx context.Context) (bool, error) {
	clientSet, err := kubernetes.NewForConfig(r.Config)
	if err != nil || clientSet == nil {
		return false, &errs.UnrecoverableError{
			Context: "Couldn't create a clientset for updating status",
			Err:     err,
		}
	}

	lckr, err := locker.NewLeaseLocker(false, *clientSet, vlanmanv1.LeaseName, r.Env.NamespaceName)
	if err != nil || lckr == nil {
		return false, &errs.UnrecoverableError{
			Context: "Couldn't create lease locker for updating status",
			Err:     err,
		}
	}

	lckr.Lock()
	networks := vlanmanv1.VlanNetworkList{}
	err = r.Client.List(ctx, &networks)
	if err != nil {
		return false, errs.NewClientRequestError("List networks", err)
	}

	requeue := false
	for _, network := range networks.Items {
		requeue, err = r.updateNetworkStatus(ctx, network)
		if err != nil {
			return false, err
		}
	}
	lckr.Unlock()
	return requeue, nil
}

func (r *VlanmanReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Starting reconciler")

	if req.Namespace == "" {
		return r.reconcileNetwork(ctx)
	}

	return r.reconcilePod(ctx, req)
}

func hasVlanmanAnnotation(obj client.Object) bool {
	a := obj.GetAnnotations()
	val, ok := a[vlanmanv1.PodVlanmanNetworkAnnotation]
	if !ok || val == "" {
		return false
	}
	return true
}

func (r *VlanmanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	annotationPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasVlanmanAnnotation(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasVlanmanAnnotation(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasVlanmanAnnotation(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasVlanmanAnnotation(e.Object)
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&vlanmanv1.VlanNetwork{}).
		Watches(&corev1.Pod{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(annotationPredicate)).
		Complete(r)
}
