package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
)

func TestManagerFromDaemonSet(t *testing.T) {
	tests := []struct {
		name        string
		daemonSet   appsv1.DaemonSet
		expectedMgr ManagerSet
	}{
		{
			name: "basic daemonset conversion",
			daemonSet: appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "manager-net1",
					Namespace: "default",
					Labels: map[string]string{
						vlanmanv1.ManagerSetLabelKey: "net1",
					},
				},
				Spec: appsv1.DaemonSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Env: []corev1.EnvVar{
										{
											Name:  "VLAN_ID",
											Value: "100",
										},
									},
								},
							},
							Affinity: &corev1.Affinity{
								NodeAffinity: &corev1.NodeAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
										NodeSelectorTerms: []corev1.NodeSelectorTerm{
											{
												MatchExpressions: []corev1.NodeSelectorRequirement{
													{
														Key:      "kubernetes.io/hostname",
														Operator: corev1.NodeSelectorOpNotIn,
														Values:   []string{"excluded-node"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedMgr: ManagerSet{
				OwnerNetworkName: "net1",
				VlanID:           100,
				ExcludedNodes:    []string{"excluded-node"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := managerFromSet(tt.daemonSet)
			assert.Equal(t, tt.expectedMgr, result)
		})
	}
}

func TestCreateDesiredManagerSet(t *testing.T) {
	tests := []struct {
		name            string
		network         vlanmanv1.VlanNetwork
		expectedManager ManagerSet
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
			expectedManager: ManagerSet{
				OwnerNetworkName: "test-network",
				VlanID:           100,
				ExcludedNodes:    nil,
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
			expectedManager: ManagerSet{
				OwnerNetworkName: "complex-network",
				VlanID:           200,
				ExcludedNodes:    []string{"node1", "node2"},
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
			expectedManager: ManagerSet{
				OwnerNetworkName: "minimal-network",
				VlanID:           42,
				ExcludedNodes:    nil,
			},
		},
		{
			name: "zero VLAN ID network",
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
			expectedManager: ManagerSet{
				OwnerNetworkName: "zero-vlan-network",
				VlanID:           0,
				ExcludedNodes:    []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createDesiredManagerSet(tt.network)
			assert.Equal(t, tt.expectedManager, result)
		})
	}
}
