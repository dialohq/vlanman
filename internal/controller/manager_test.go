package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
)

func TestManagerFromPod(t *testing.T) {
	tests := []struct {
		name        string
		pod         corev1.Pod
		expectedPod ManagerPod
	}{
		{
			name: "basic pod conversion",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-manager-pod",
					Namespace: "default",
					Labels: map[string]string{
						vlanmanv1.ManagerPodLabelKey: "net1",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "node1",
				},
			},
			expectedPod: ManagerPod{
				Exists:           true,
				OwnerNetworkName: "net1",
				NodeName:         "node1",
			},
		},
		{
			name: "pod with additional labels",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-manager-pod",
					Namespace: "kube-system",
					Labels: map[string]string{
						vlanmanv1.ManagerPodLabelKey: "net2",
						"app":                        "vlanman",
						"version":                    "v1.0.0",
					},
					Annotations: map[string]string{
						"description": "VLAN manager pod",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "worker-node-1",
					Containers: []corev1.Container{
						{
							Name:  "manager",
							Image: "vlanman:latest",
						},
					},
				},
			},
			expectedPod: ManagerPod{
				Exists:           true,
				OwnerNetworkName: "net2",
				NodeName:         "worker-node-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := managerFromPod(tt.pod)
			assert.Equal(t, tt.expectedPod, result)
		})
	}
}

func TestCreateDesiredManager(t *testing.T) {
	tests := []struct {
		name            string
		network         vlanmanv1.VlanNetwork
		expectedManager ManagerPod
		node            string
	}{
		{
			name: "basic network",
			network: vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-network",
					Namespace: "default",
				},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:       100,
					GatewayIP:    "192.168.1.1",
					LocalSubnet:  "192.168.1.0/24",
					RemoteSubnet: "192.168.2.0/24",
				},
			},
			node: "test-node",
			expectedManager: ManagerPod{
				OwnerNetworkName: "test-network",
				Exists:           true,
				NodeName:         "test-node",
			},
		},
		{
			name: "network with excluded nodes",
			network: vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-network",
					Namespace: "kube-system",
				},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:        200,
					GatewayIP:     "10.0.0.1",
					LocalSubnet:   "10.0.0.0/16",
					RemoteSubnet:  "10.1.0.0/16",
					ExcludedNodes: []string{"node1", "node2"},
				},
			},
			node: "test-node",
			expectedManager: ManagerPod{
				Exists:           true,
				OwnerNetworkName: "complex-network",
				NodeName:         "test-node",
			},
		},
		{
			name: "minimal network",
			network: vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "minimal-network",
				},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID: 42,
				},
			},
			node: "test-node",
			expectedManager: ManagerPod{
				Exists:           true,
				OwnerNetworkName: "minimal-network",
				NodeName:         "test-node",
			},
		},
		{
			name: "network with zero VLAN ID",
			network: vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "zero-vlan-network",
				},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:        0,
					GatewayIP:     "172.16.0.1",
					LocalSubnet:   "172.16.0.0/12",
					RemoteSubnet:  "172.17.0.0/12",
					ExcludedNodes: []string{},
				},
			},
			node: "test-node",
			expectedManager: ManagerPod{
				Exists:           true,
				OwnerNetworkName: "zero-vlan-network",
				NodeName:         "test-node",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createDesiredManager(tt.network, tt.node)
			assert.Equal(t, tt.expectedManager, result)
		})
	}
}
