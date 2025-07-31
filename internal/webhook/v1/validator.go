package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	errs "dialo.ai/vlanman/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Validator struct {
	Nodes    []corev1.Node
	Pods     []corev1.Pod
	Networks []vlanmanv1.VlanNetwork
}

func (v *Validator) validateMinimumNodes(net *vlanmanv1.VlanNetwork) error {
	if len(v.Nodes) == 0 {
		return fmt.Errorf("There are no available nodes (make sure you don't exclude all nodes)")
	}

	// Check if ManagerAffinity would exclude all nodes
	if net.Spec.ManagerAffinity != nil && 
		net.Spec.ManagerAffinity.NodeAffinity != nil && 
		net.Spec.ManagerAffinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		
		availableNodes := 0
		for _, node := range v.Nodes {
			nodeMatches := false
			for _, term := range net.Spec.ManagerAffinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "kubernetes.io/hostname" && expr.Operator == corev1.NodeSelectorOpNotIn {
						// Check if this node is in the excluded list
						for _, excludedNode := range expr.Values {
							if node.Name == excludedNode {
								nodeMatches = true
								break
							}
						}
						if nodeMatches {
							break
						}
					}
				}
				if nodeMatches {
					break
				}
			}
			if !nodeMatches {
				availableNodes++
			}
		}
		
		if availableNodes == 0 {
			return fmt.Errorf("There are no available nodes (make sure you don't exclude all nodes)")
		}
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

func NewUpdateValidator(k8s client.Client, ctx context.Context, newObj, oldObj []byte) (*UpdateValidator, error) {
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
	if err = json.Unmarshal(newObj, newVlanNetwork); err != nil {
		return nil, fmt.Errorf("Couldn't unmarshal vlan network for Update: %w", err)
	}
	oldVlanNetwork := &vlanmanv1.VlanNetwork{}
	if err = json.Unmarshal(oldObj, oldVlanNetwork); err != nil {
		return nil, fmt.Errorf("Couldn't unmarshal vlan network for update: %w", err)
	}

	validator := &Validator{
		Nodes:    allNodes.Items,
		Pods:     allPods.Items,
		Networks: vlanNetworks.Items,
	}
	return &UpdateValidator{
		Validator:  validator,
		NewNetwork: newVlanNetwork,
		OldNetwork: oldVlanNetwork,
	}, nil
}

func (uv *UpdateValidator) Validate() error {
	err := uv.validateMinimumNodes(uv.NewNetwork)
	if err != nil {
		return fmt.Errorf("Couldn't validate minimum node requirement: %w", err)
	}

	// don't allow any changes besides pools
	uv.NewNetwork.Spec.Pools = nil
	uv.OldNetwork.Spec.Pools = nil
	if !reflect.DeepEqual(uv.NewNetwork.Spec, uv.OldNetwork.Spec) {
		return fmt.Errorf("The only field in spec that supports update is 'pools'.")
	}
	return nil
}

type DeletionValidator struct {
	*Validator
	DeletedNetwork *vlanmanv1.VlanNetwork
}

func NewDeletionValidator(k8s client.Client, ctx context.Context, obj []byte) (*DeletionValidator, error) {
	toDeleteVlanNetwork := &vlanmanv1.VlanNetwork{}
	if err := json.Unmarshal(obj, toDeleteVlanNetwork); err != nil {
		return nil, fmt.Errorf("Couldn't unmarshal vlan network for deletion: %w", err)
	}
	relevantPods := &corev1.PodList{}
	req, err := labels.NewRequirement(vlanmanv1.WorkerPodLabelKey, "==", []string{toDeleteVlanNetwork.Name})
	if err != nil || req == nil {
		return nil, &errs.InternalError{Context: "Couldn't create a label requirement to list pods in  NewDeletionValidator"}
	}
	err = k8s.List(ctx, relevantPods, &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*req),
	})
	if err != nil {
		return nil, fmt.Errorf("Error listing pods: %w", err)
	}
	validator := &Validator{
		Pods: relevantPods.Items,
	}
	return &DeletionValidator{
		Validator:      validator,
		DeletedNetwork: toDeleteVlanNetwork,
	}, nil
}

var ErrNetInUse error = errors.New("Network is still used by pods")

type NetInUseError struct {
	Pods []string
}

func (e *NetInUseError) Error() string {
	return fmt.Sprintf("Network is still used by %d pods: %s", len(e.Pods), strings.Join(e.Pods, ", "))
}

func (e *NetInUseError) Unwrap() error {
	return ErrNetInUse
}

func (cv *DeletionValidator) validateNotInUse() error {
	if len(cv.Pods) == 0 {
		return nil
	}
	names := []string{}
	for _, p := range cv.Pods {
		s := p.Status.Phase
		if s == corev1.PodPending || s == corev1.PodRunning {
			names = append(names, strings.Join([]string{p.Name, p.Namespace}, "@"))
		}
	}
	if len(names) != 0 {
		return &NetInUseError{Pods: names}
	}
	return nil
}

func (cv *DeletionValidator) Validate() error {
	return cv.validateNotInUse()
}
