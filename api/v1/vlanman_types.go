package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=vlan
// +kubebuilder:subresource:status

type VlanNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VlanNetworkSpec   `json:"spec,omitempty"`
	Status VlanNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VlanNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []VlanNetwork `json:"items"`
}

type VlanNetworkSpec struct {
	// LocalGatewayIP specifies the IP address of the local gateway for the VLAN network
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	// +optional
	LocalGatewayIP string `json:"localGatewayIp"`
	// RemoteGatewayIP specifies the IP address of the remote gateway for the VLAN network
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	// +optional
	RemoteGatewayIP string `json:"remoteGatewayIp"`
	// LocalSubnet defines the list of local subnet CIDR blocks
	// +kubebuilder:validation:MinItems=1
	LocalSubnet []string `json:"localSubnet"`
	// RemoteSubnet defines the list of remote subnet CIDR blocks
	// +kubebuilder:validation:MinItems=1
	RemoteSubnet []string `json:"remoteSubnet"`
	// VlanID specifies the VLAN identifier (1-4094)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4094
	VlanID int `json:"vlanId"`
	// ManagerAffinity defines node affinity rules for the VLAN manager pods
	// +optional
	ManagerAffinity *corev1.Affinity `json:"managerAffinity,omitempty"`
	// Pools defines the IP address pools available for allocation in this VLAN network
	// +kubebuilder:validation:MinItems=1
	Pools []VlanNetworkPool `json:"pools"`
	// Mappings defines the node-to-interface mappings for this VLAN network
	// +optional
	Mappings []IPMapping `json:"mappings"`
}

type IPMapping struct {
	// NodeName specifies the name of the Kubernetes node
	// +kubebuilder:validation:MinLength=1
	NodeName string `json:"nodeName"`
	// Interface specifies the network interface name on the node
	// +kubebuilder:validation:MinLength=1
	Interface string `json:"interfaceName"`
}

type VlanNetworkPool struct {
	// Description provides a human-readable description of the IP pool
	Description string `json:"description"`
	// Addresses contains the list of IP addresses or CIDR blocks in this pool
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`
	// Name is the unique identifier for this IP pool
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type VlanNetworkStatus struct {
	// FreeIPs contains available IP addresses grouped by pool name
	FreeIPs map[string][]string `json:"freeIPs"`
	// PendingIPs contains IP addresses that are pending allocation, grouped by pool and request
	PendingIPs map[string]map[string]string `json:"pendingIPs"`
}

func init() {
	SchemeBuilder.Register(&VlanNetwork{}, &VlanNetworkList{})
}
