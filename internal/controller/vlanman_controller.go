package controller

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
}

type VlanmanReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
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
	Errs []ReconcileError
}

func (e *ReconcileErrorList) Error() string {
	msg := fmt.Sprintf("%d errors occured while reconciling: ", len(e.Errs))
	formatted := []string{}
	for idx, err := range e.Errs {
		formatted = append(formatted, fmt.Sprintf("%d -> %s, ", idx, err.Error()))
	}
	return msg + strings.Join(formatted, ", ")
}

func (r *VlanmanReconciler) ReconcilePod(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	return ctrl.Result{}, &errs.UnimplementedError{Feature: "Pod reconciliation"}
}

func (r *VlanmanReconciler) getCurrentState(ctx context.Context) (*ClusterState, error) {
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement(vlanmanv1.ManagerPodLabelKey, "exists", nil)
	if err != nil {
		return nil, &errs.InternalError{Context: fmt.Sprintf("Error creating a label selector requirement in createDesiredState: %s", err.Error())}
	}
	selector = selector.Add(*requirement)

	opts := client.ListOptions{
		LabelSelector: selector,
	}
	managerList := &corev1.PodList{}
	err = r.Client.List(ctx, managerList, &opts)
	if err != nil {
		return nil, &errs.ClientRequestError{
			Location: "getCurrentState",
			Action:   "List Pods with Field Selector",
			Err:      err,
		}
	}
	state := &ClusterState{Nodes: map[string]Node{}}
	for _, manager := range managerList.Items {
		nodeName := manager.Spec.NodeName
		node, ok := state.Nodes[nodeName]
		if !ok {
			node = Node{Managers: map[string]ManagerPod{}}
		}

		nwName, ok := manager.Labels[vlanmanv1.ManagerPodLabelKey]
		if !ok {
			return nil, &errs.InternalError{Context: "In getCurrentState, pod has no label"}
		}
		mgr := managerFromPod(manager)
		node.Managers[nwName] = mgr
		state.Nodes[nodeName] = node
	}
	return state, nil
}

func (r *VlanmanReconciler) createDesiredState(ctx context.Context, networks []vlanmanv1.VlanNetwork) (*ClusterState, error) {
	nodeList := &corev1.NodeList{}
	err := r.Client.List(ctx, nodeList)
	if err != nil {
		return nil, &errs.ClientRequestError{
			Location: "createDesiredState",
			Action:   "List Nodes",
			Err:      err,
		}
	}
	state := &ClusterState{Nodes: map[string]Node{}}
	for _, node := range nodeList.Items {
		nodeState, ok := state.Nodes[node.Name]
		if !ok {
			nodeState = Node{Managers: map[string]ManagerPod{}}
		}
		for _, nw := range networks {
			if slices.Contains(nw.Spec.ExcludedNodes, node.Name) {
				continue
			}
			nodeState.Managers[nw.Name] = createDesiredManager(nw, node.Name)
		}
		state.Nodes[node.Name] = nodeState
	}
	return state, nil
}

func (r *VlanmanReconciler) diffManagers(desired, current *ManagerPod) []Action {
	if reflect.DeepEqual(desired, current) {
		return []Action{}
	}

	if desired != nil && current == nil {
		return []Action{&CreateManagerAction{Manager: *desired}}
	}

	if desired != nil && current != nil {
		return []Action{&ThrowErrorAction{
			Err: &errs.InternalError{
				Context: fmt.Sprintf("in diffManagers, both are not nil but aren't equal. Currently there is no valid case in which this happens: %+v, %+v", desired, current),
			},
		}}
	}
	return []Action{&ThrowErrorAction{
		Err: &errs.InternalError{
			Context: fmt.Sprintf("In diffManagers got to the end of function, this shouldn't happen. Should always exit before: %+v, %+v", desired, current),
		},
	}}
}

func (r *VlanmanReconciler) diffNodes(desired, current Node) []Action {
	acts := []Action{}
	if reflect.DeepEqual(desired, current) {
		return acts
	}

	for nwName, desiredMgr := range desired.Managers {
		currentMgr, ok := current.Managers[nwName]
		if !ok {
			acts = append(acts, r.diffManagers(&desiredMgr, nil)...)
		} else {
			acts = append(acts, r.diffManagers(&desiredMgr, &currentMgr)...)
		}
	}

	desiredAll := slices.Collect(maps.Keys(desired.Managers))
	currentAll := slices.Collect(maps.Keys(current.Managers))
	for _, c := range currentAll {
		if !slices.Contains(desiredAll, c) {
			acts = append(acts, &DeleteManagerAction{Manager: current.Managers[c]})
		}
	}
	return acts
}

func (r *VlanmanReconciler) diffStates(desired, current *ClusterState) []Action {
	acts := []Action{}
	for nodeName, desiredState := range desired.Nodes {
		currentState := current.Nodes[nodeName]
		acts = append(acts, r.diffNodes(desiredState, currentState)...)
	}
	return acts
}

func (r *VlanmanReconciler) ReconcileNetwork(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	networkList := &vlanmanv1.VlanNetworkList{}
	err := r.Client.List(ctx, networkList)
	if err != nil {
		return ctrl.Result{}, &errs.ClientRequestError{
			Location: "ReconcileNetwork",
			Action:   "List VlanNetworks",
			Err:      err,
		}
	}

	desired, err := r.createDesiredState(ctx, networkList.Items)
	if err != nil {
		return ctrl.Result{}, err
	}

	current, err := r.getCurrentState(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

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
		return ctrl.Result{}, nil
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
	var annotationPredicate = predicate.Funcs{
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
