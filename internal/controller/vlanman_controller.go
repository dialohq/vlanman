package controller

import (
	"context"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

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

func (r *VlanmanReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling")

	return ctrl.Result{}, nil
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
