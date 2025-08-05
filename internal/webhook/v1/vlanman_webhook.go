package v1

import (
	"context"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"
	errs "dialo.ai/vlanman/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// TODO: merge into one i guess
func SetupVlanmanWebhookWithManager(mgr ctrl.Manager, e controller.Envs) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&vlanmanv1.VlanNetwork{}).
		WithValidator(&VlanmanCustomValidator{
			Client: mgr.GetClient(),
			Config: *mgr.GetConfig(),
			Env:    e,
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-vlanman-dialo-ai-v1-vlannetwork,mutating=false,failurePolicy=fail,sideEffects=None,groups=vlanman.dialo.ai,resources=vlannetworks,verbs=create;update;delete,versions=v1,name=webhook.vlanman.dialo.ai,admissionReviewVersions=v1,serviceName=replaceme[.Values.webhook.serviceName],servicePort=443,serviceNamespace=replaceme[.Values.global.namespace]

type VlanmanCustomValidator struct {
	Client client.Client
	Config rest.Config
	Env    controller.Envs
}

var _ webhook.CustomValidator = &VlanmanCustomValidator{}

func (v *VlanmanCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	network, ok := obj.(*vlanmanv1.VlanNetwork)
	if !ok {
		return nil, errs.NewTypeMismatchError("Validating creation", obj)
	}
	validator, err := NewDeletionValidator(v.Client, ctx, network)
	if err != nil {
		return nil, err
	}
	err = validator.Validate()
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (v *VlanmanCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	network, ok := obj.(*vlanmanv1.VlanNetwork)
	if !ok {
		return nil, errs.NewTypeMismatchError("Validating deletion", obj)
	}
	validator, err := NewDeletionValidator(v.Client, ctx, network)
	if err != nil {
		return nil, err
	}
	err = validator.Validate()
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (v *VlanmanCustomValidator) ValidateUpdate(ctx context.Context, objOld, objNew runtime.Object) (admission.Warnings, error) {
	networkOld, ok := objOld.(*vlanmanv1.VlanNetwork)
	if !ok {
		return nil, errs.NewTypeMismatchError("Validating update", objOld)
	}
	networkNew, ok := objNew.(*vlanmanv1.VlanNetwork)
	if !ok {
		return nil, errs.NewTypeMismatchError("Validating update", objNew)
	}

	validator, err := NewUpdateValidator(v.Client, ctx, networkNew, networkOld)
	if err != nil {
		return nil, err
	}
	err = validator.Validate()
	if err != nil {
		return nil, err
	}
	return nil, nil
}
