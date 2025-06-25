package controller

import (
	"context"
	"fmt"

	errs "dialo.ai/vlanman/pkg/errors"
)

type Action interface {
	Do(context.Context, *VlanmanReconciler) error
}

type CreateManagerAction struct {
	Manager ManagerPod
}

func (a *CreateManagerAction) Do(ctx context.Context, r *VlanmanReconciler) error {
	fmt.Println("Creating manager: ", a.Manager.OwnerNetworkName)
	pod := podFromManager(a.Manager, r.Env)
	err := r.Client.Create(ctx, &pod)
	if err != nil {
		return &errs.ClientRequestError{
			Location: "CreateManagerAction",
			Action:   "Create pod",
			Err:      err,
		}
	}
	return nil
}

type DeleteManagerAction struct {
	Manager ManagerPod
}

func (a *DeleteManagerAction) Do(ctx context.Context, r *VlanmanReconciler) error {
	fmt.Println("Deleting manager: ", a.Manager.OwnerNetworkName)
	return nil
}

type ThrowErrorAction struct {
	Err error
}

func (a *ThrowErrorAction) Do(ctx context.Context, r *VlanmanReconciler) error {
	return a.Err
}
