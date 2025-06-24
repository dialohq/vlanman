package v1

import (
	"context"
	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
)

type Validator struct {
	Nodes    []corev1.Node
	Pods     []corev1.Pod
	Networks []vlanmanv1.VlanNetwork
}

func (v *Validator) validateMinimumNodes(net *vlanmanv1.VlanNetwork) error {
	names := make([]string, len(v.Nodes))
	for i, n := range v.Nodes {
		names[i] = n.Name
	}

	for _, n := range net.Spec.ExcludedNodes {
		names = slices.DeleteFunc(names, func(el string) bool {
			return el == n
		})
	}
	
	if len(names) == 0 {
		return fmt.Errorf("There are no available nodes (make sure you don't exclude all nodes)")
	}

	return nil
}

func (v *Validator) validateUnique(net *vlanmanv1.VlanNetwork) error {
	for _, nw := range v.Networks {
		if nw.Spec.VlanID == net.Spec.VlanID {
			return fmt.Errorf("There exists a network with that VLAN ID: %s", nw.Name)
		}
	}
	return nil
}

type ValidatorInterface interface {
	validate(ctx context.Context) error
}

type CreationValidator struct {
	*Validator
	NewNetwork *vlanmanv1.VlanNetwork
}

func NewCreationValidator(k8s client.Client, ctx context.Context, obj []byte) (*CreationValidator, error) {
	allPods := &corev1.PodList{}
	err := k8s.List(ctx, allPods)
	if err != nil {
		return nil, fmt.Errorf("Error listing pods: %w", err)
	}
	allNodes := &corev1.NodeList{}
	err = k8s.List(ctx, allNodes)
	if err != nil {
		return nil, fmt.Errorf("Error listing nodes: %w", err)
	}
	vlanNetworks := &vlanmanv1.VlanNetworkList{}
	err = k8s.List(ctx, vlanNetworks)
	if err != nil {
		return nil, fmt.Errorf("Error listing VlanNetworks: %w", err)
	}
	newVlanNetwork := &vlanmanv1.VlanNetwork{}
	if err = json.Unmarshal(obj, newVlanNetwork); err != nil {
		return nil, fmt.Errorf("Couldn't unmarshal vlan network for creation: %w", err)
	}
	validator := &Validator{
		Nodes:    allNodes.Items,
		Pods:     allPods.Items,
		Networks: vlanNetworks.Items,
	}
	return &CreationValidator{
		Validator:  validator,
		NewNetwork: newVlanNetwork,
	}, nil
}

func (cv *CreationValidator) Validate() error {
	err := cv.validateMinimumNodes(cv.NewNetwork)
	if err != nil {
		return fmt.Errorf("Couldn't validate minimum node requirement: %w", err)
	}
	return cv.validateUnique(cv.NewNetwork)
}

type UpdateValidator struct {
	*Validator
	OldNetwork *vlanmanv1.VlanNetwork
	NewNetwork *vlanmanv1.VlanNetwork
}

type DeletionValidator struct {
	*Validator
	DeletedNetwork *vlanmanv1.VlanNetwork
}
