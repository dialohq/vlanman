package controller

import (
	"context"
	"fmt"
	"slices"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"github.com/alecthomas/repr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	errs "dialo.ai/vlanman/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			Location: "getCurrentPod",
			Action:   "List Pods with Field Selector",
			Err:      err,
		}
	}
	state := &ClusterState{Nodes: map[string]Node{}}
	for _, manager := range managerList.Items {
		nodeName := manager.Spec.NodeName
		node, ok := state.Nodes[nodeName]
		if !ok {
			node = Node{Managers: []ManagerPod{}}
		}

		node.Managers = append(node.Managers, managerFromPod(manager))
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
			nodeState = Node{Managers: []ManagerPod{}}
		}
		for _, nw := range networks {
			if slices.Contains(nw.Spec.ExcludedNodes, node.Name) {
				continue
			}
			nodeState.Managers = append(nodeState.Managers, createDesiredManager(nw))
		}
		state.Nodes[node.Name] = nodeState
	}
	return state, nil
}

func (r *VlanmanReconciler) ReconcileNetwork(ctx context.Context, network *vlanmanv1.VlanNetwork) (reconcile.Result, error) {
	if network == nil {
		return ctrl.Result{}, &errs.InternalError{Context: "In 'ReconcileNetwork', network is nil"}
	}

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

	fmt.Println("Desired")
	repr.Println(desired)
	current, err := r.getCurrentState(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	fmt.Println("Current")
	repr.Println(current)

	return ctrl.Result{}, nil
}

func (r *VlanmanReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Starting reconciler")

	network := &vlanmanv1.VlanNetwork{}
	err := r.Client.Get(ctx, req.NamespacedName, network)
	if apierrors.IsNotFound(err) {
		return r.ReconcilePod(ctx, req)
	}

	if err != nil {
		return ctrl.Result{}, &errs.ClientRequestError{
			Location: "Reconciler",
			Action:   "GET VlanNetwork",
			Err:      err,
		}
	}

	return r.ReconcileNetwork(ctx, network)
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
