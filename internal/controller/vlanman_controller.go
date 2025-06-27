package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
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
}

type VlanmanReconciler struct {
	Client client.Client
	Scheme *k8sRuntime.Scheme
	Env    Envs
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

func (r *VlanmanReconciler) ReconcilePod(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	return ctrl.Result{}, &errs.UnimplementedError{Feature: "Pod reconciliation"}
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
		mgrs = append(mgrs, ManagerSet{
			OwnerNetworkName: net.Name,
			VlanID:           int64(net.Spec.VlanID),
			ExcludedNodes:    net.Spec.ExcludedNodes,
		})
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
		fmt.Println("These are the same manager: ", desiredMgr.OwnerNetworkName, current[idx].OwnerNetworkName)
		acts = append(acts, r.diffManagers(desiredMgr, current[idx])...)
	}

	for _, currentMgr := range current {
		_, found := slices.BinarySearchFunc(desired, currentMgr, managerCmp)
		if !found {
			fmt.Println("this manager is not in desired state: ", currentMgr)
			fmt.Println("Desired staet: ", desired)
			acts = append(acts, &DeleteManagerAction{Manager: currentMgr})
		}
	}
	return acts
}

func (r *VlanmanReconciler) ReconcileNetwork(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	networkList := &vlanmanv1.VlanNetworkList{}
	err := r.Client.List(ctx, networkList)
	if err != nil {
		return ctrl.Result{}, &errs.ClientRequestError{
			Action: "List VlanNetworks",
			Err:    err,
		}
	}

	fmt.Println("Creating desired")
	desired := r.createDesiredState(networkList.Items)

	fmt.Println("Getting current")
	current, err := r.getCurrentState(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	fmt.Println("Diffing")
	actions := r.diffStates(desired, current)

	errList := []*ReconcileError{}
	if len(actions) == 0 {
		log.Info("No actions to take")
		return ctrl.Result{}, nil
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

	return ctrl.Result{}, nil
}

func (r *VlanmanReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Starting reconciler")
	if req.Namespace == "" {
		return r.ReconcileNetwork(ctx, req)
	}

	return r.ReconcilePod(ctx, req)
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
