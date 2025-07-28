package controller

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	errs "dialo.ai/vlanman/pkg/errors"
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
	Client         client.Client
	Scheme         *k8sRuntime.Scheme
	Env            Envs
	Config         *rest.Config
	reconciles     atomic.Int64
	fullReconciles atomic.Int64
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

func (r *VlanmanReconciler) getCurrentState(ctx context.Context) ([]ManagerSet, error) {
	managers := appsv1.DaemonSetList{}
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement(vlanmanv1.ManagerSetLabelKey, "exists", nil)
	if err != nil {
		return nil, &errs.InternalError{
			Context: fmt.Sprintf("Error creating a label selector requirement in createDesiredState: %s", err.Error()),
		}
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

func (r *VlanmanReconciler) reconcileNetwork(ctx context.Context) (*time.Duration, error) {
	log := log.FromContext(ctx)

	errList := []*ReconcileError{}

	networkList := &vlanmanv1.VlanNetworkList{}
	err := r.Client.List(ctx, networkList)
	if err != nil {
		return nil, &errs.ClientRequestError{
			Action: "List VlanNetworks",
			Err:    err,
		}
	}

	desired := r.createDesiredState(networkList.Items)

	current, err := r.getCurrentState(ctx)
	if err != nil {
		return nil, err
	}

	actions := r.diffStates(desired, current)

	for _, action := range actions {
		log.Info("Doing action", "type", reflect.TypeOf(action))
		err := action.Do(ctx, r)
		if err != nil {
			recErr := &ReconcileError{Action: reflect.TypeOf(action), Err: err}
			errList = append(errList, recErr)
			log.Error(recErr, "Error reconciling")
		}
	}

	if len(errList) != 0 {
		return nil, &ReconcileErrorList{
			Errs: errList,
		}
	}

	return nil, err
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

func poolName(name string) func(vlanmanv1.VlanNetworkPool) bool {
	return func(p vlanmanv1.VlanNetworkPool) bool {
		return name == p.Name
	}
}

func (r *VlanmanReconciler) updateVlanNetworkStatus(ctx context.Context, net *vlanmanv1.VlanNetwork) (*time.Duration, error) {
	if net.Status.FreeIPs == nil {
		net.Status.FreeIPs = map[string][]string{}
	}
	if net.Status.PendingIPs == nil {
		net.Status.PendingIPs = map[string]map[string]string{}
	}

	for name := range net.Status.FreeIPs {
		if !slices.ContainsFunc(net.Spec.Pools, poolName(name)) {
			delete(net.Status.FreeIPs, name)
			delete(net.Status.PendingIPs, name)
		}
	}

	for _, pool := range net.Spec.Pools {
		if net.Status.FreeIPs[pool.Name] == nil {
			net.Status.FreeIPs[pool.Name] = []string{}
		}
		net.Status.FreeIPs[pool.Name] = slices.Clone(pool.Addresses)

		if net.Status.PendingIPs[pool.Name] == nil {
			net.Status.PendingIPs[pool.Name] = map[string]string{}
		}
	}

	podlist := &corev1.PodList{}
	requirement, err := labels.NewRequirement(vlanmanv1.WorkerPodLabelKey, "==", []string{net.Name})
	if err != nil {
		return nil, &errs.InternalError{
			Context: fmt.Sprintf("Error compiling the label selector requirement for listing worker pods while updating status: %s", err.Error()),
		}
	}
	selector := labels.NewSelector().Add(*requirement)
	err = r.Client.List(ctx, podlist, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		err = errs.NewClientRequestError("Error listing pods in updateVlanNetworkStatus", err)
		return nil, err
	}
	podsWithAnnotation := podlist.Items

	// TODO: if it turns out it's taking too long we can reverse sort by
	// timestamp and stop after we reach the first still valid
	for name := range net.Status.PendingIPs {
		maps.DeleteFunc(net.Status.PendingIPs[name], func(ip string, timestamp string) bool {
			ts, err := time.Parse(time.Layout, timestamp)
			if err != nil {
				err = errs.NewParsingError(fmt.Sprintf("Pending IP timestamp '%s' in updateVlanNetworkStatus", timestamp), err)
				return true
			}
			contains := slices.ContainsFunc(podsWithAnnotation, func(p corev1.Pod) bool {
				extractedIP, found := extractVlan(p)
				cutVip, _, _ := strings.Cut(extractedIP, "/")
				cutIp, _, _ := strings.Cut(ip, "/")

				return found && cutIp == cutVip
			})
			timePassed := time.Now().After(ts.Add(time.Second * time.Duration(vlanmanv1.ReconcilerPendingIPsTimeoutSeconds)))
			return contains || timePassed
		})
	}

	podIpList := []string{}
	for _, p := range podsWithAnnotation {
		ip, found := extractVlan(p)
		cutIp, _, _ := strings.Cut(ip, "/")
		if found {
			podIpList = append(podIpList, cutIp)
		}
	}

	for pn := range net.Status.FreeIPs {
		if net.Status.FreeIPs[pn] == nil {
			net.Status.FreeIPs[pn] = []string{}
		}
		pendingIPs := slices.Collect(maps.Keys(net.Status.PendingIPs))
		net.Status.FreeIPs[pn] = slices.DeleteFunc(net.Status.FreeIPs[pn], func(ip string) bool {
			cutIp, _, _ := strings.Cut(ip, "/")
			return slices.Contains(pendingIPs, cutIp) || slices.Contains(podIpList, cutIp)
		})
	}

	var requeueIn *time.Duration
	for _, pending := range net.Status.PendingIPs {
		p := slices.Collect(maps.Values(pending))
		sortedPending := []time.Duration{}
		for _, v := range p {
			// err ignored since already checked above
			// maybe combine it if there is a lot of them :TODO
			t, _ := time.Parse(time.Layout, v)
			sortedPending = append(sortedPending, time.Until(t.Add(time.Second*vlanmanv1.ReconcilerPendingIPsTimeoutSeconds)))
		}
		if len(sortedPending) == 0 {
			requeueIn = nil
		} else {
			min := slices.Min(sortedPending)
			requeueIn = &min
		}
	}
	return requeueIn, nil
}

func (r *VlanmanReconciler) UpdateAllNetworkStatus(ctx context.Context) (*time.Duration, error) {
	list := vlanmanv1.VlanNetworkList{}
	var rq *time.Duration
	err := r.Client.List(ctx, &list)
	if err != nil {
		err = errs.NewClientRequestError("List vlan networks in UpdateStatus", err)
		return nil, err
	}
	for _, net := range list.Items {
		rq2, err := r.updateVlanNetworkStatus(ctx, &net)
		rq = rq2
		if err != nil {
			return rq, err
		}
		err = r.Client.Status().Update(ctx, &net)
		if err != nil {
			err = errs.NewClientRequestError(fmt.Sprintf("Update vlan network %s's status", net.Name), err)
			return rq, err
		}
	}
	return rq, nil
}

func (r *VlanmanReconciler) ensurePodMonitor(ctx context.Context) error {
	prtNum := int32(vlanmanv1.ManagerPodAPIPort)
	prtName := vlanmanv1.ManagerPodAPIPortName
	err := r.Client.Create(ctx, &promv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vlanmanv1.PodMonitorName,
			Namespace: r.Env.NamespaceName,
			Labels: map[string]string{
				"release": r.Env.MonitoringReleaseName,
			},
		},
		Spec: promv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      vlanmanv1.ManagerSetLabelKey,
					Operator: metav1.LabelSelectorOpExists,
				},
			}},
			PodMetricsEndpoints: []promv1.PodMetricsEndpoint{
				{
					Port:       &prtName,
					PortNumber: &prtNum,
					Path:       "/metrics",
					Interval:   promv1.Duration(r.Env.MonitoringScrapeInterval),
				},
			},
		},
	})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return errs.NewClientRequestError("CreatePodMonitor", err)
		}
	}
	return nil
}

var done bool = false

func (r *VlanmanReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Starting reconciler")

	if r.Env.IsMonitoringEnabled {
		r.ensurePodMonitor(ctx)
	}

	pod := corev1.Pod{}
	err := r.Client.Get(ctx, req.NamespacedName, &pod)
	if !apierrors.IsNotFound(err) {
		if err != nil {
			log.Error(err, "Error fetching pod")
			return ctrl.Result{}, err
		} else {
			if pod.Status.PodIP == "" {
				log.Info("Pod doesn't have ip yet, requeuing...")
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
		}
	}

	rq, err := r.UpdateAllNetworkStatus(ctx)
	ctr := 1
	for apierrors.IsConflict(err) && ctr <= vlanmanv1.UpdateStatusMaxRetries {
		log.Info("Error updating status, trying again", "tries", fmt.Sprintf("%d/%d", ctr, vlanmanv1.UpdateStatusMaxRetries), "error", err)
		rq, err = r.UpdateAllNetworkStatus(ctx)
		ctr += 1
	}

	if err != nil {
		if ctr == vlanmanv1.UpdateStatusMaxRetries+1 {
			return ctrl.Result{}, fmt.Errorf("Error updating status after %d tries: %w", vlanmanv1.UpdateStatusMaxRetries, err)
		}
	}

	var rqNet *time.Duration
	if req.Namespace == "" {
		rqNet, err = r.reconcileNetwork(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	if rqNet != nil {
		return res(rq, *rqNet), nil
	}
	return res(rq), nil
}

func res(rq *time.Duration, times ...time.Duration) ctrl.Result {
	if rq == nil && times == nil {
		return ctrl.Result{}
	}
	if rq == nil {
		return ctrl.Result{RequeueAfter: slices.Min(times)}
	}

	times = append(times, *rq)
	min := slices.Min(times)

	return ctrl.Result{RequeueAfter: min}
}

func hasVlanmanAnnotation(obj client.Object) bool {
	a := obj.GetAnnotations()
	val, ok := a[vlanmanv1.PodVlanmanNetworkAnnotation]
	if !ok || val == "" {
		return false
	}
	return true
}

func notJob(obj client.Object) bool {
	_, ok := obj.GetLabels()["job-name"]
	return !ok
}

func (r *VlanmanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	annotationPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasVlanmanAnnotation(e.Object) && notJob(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasVlanmanAnnotation(e.Object) && notJob(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasVlanmanAnnotation(e.Object) && notJob(e.Object)
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&vlanmanv1.VlanNetwork{}).
		Watches(&corev1.Pod{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(annotationPredicate)).
		Complete(r)
}
