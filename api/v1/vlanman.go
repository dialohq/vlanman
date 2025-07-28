package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VlanNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VlanNetworkSpec   `json:"spec,omitempty"`
	Status VlanNetworkStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VlanNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []VlanNetwork `json:"items"`
}

type VlanNetworkSpec struct {
	LocalGatewayIP  string            `json:"localGatewayIp"`
	RemoteGatewayIP string            `json:"remoteGatewayIp"`
	LocalSubnet     []string          `json:"localSubnet"`
	RemoteSubnet    []string          `json:"remoteSubnet"`
	VlanID          int               `json:"vlanId"`
	ExcludedNodes   []string          `json:"excludedNodes,omitempty"`
	Pools           []VlanNetworkPool `json:"pools"`
}

type VlanNetworkPool struct {
	Description string   `json:"description"`
	Addresses   []string `json:"addresses"`
	Name        string   `json:"name"`
}

type VlanNetworkStatus struct {
	FreeIPs    map[string][]string          `json:"freeIPs"`
	PendingIPs map[string]map[string]string `json:"pendingIPs"`
}

func init() {
	SchemeBuilder.Register(&VlanNetwork{}, &VlanNetworkList{})
}
