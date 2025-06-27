package v1

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
)

func TestValidator_validateMinimumNodes(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []corev1.Node
		network       *vlanmanv1.VlanNetwork
		expectedError bool
		errorContains string
	}{
		{
			name: "valid - no excluded nodes",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			network: &vlanmanv1.VlanNetwork{
				Spec: vlanmanv1.VlanNetworkSpec{
					ExcludedNodes: []string{},
				},
			},
			expectedError: false,
		},
		{
			name: "valid - some nodes excluded",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			network: &vlanmanv1.VlanNetwork{
				Spec: vlanmanv1.VlanNetworkSpec{
					ExcludedNodes: []string{"node1"},
				},
			},
			expectedError: false,
		},
		{
			name: "invalid - all nodes excluded",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			network: &vlanmanv1.VlanNetwork{
				Spec: vlanmanv1.VlanNetworkSpec{
					ExcludedNodes: []string{"node1", "node2"},
				},
			},
			expectedError: true,
			errorContains: "There are no available nodes",
		},
		{
			name:  "invalid - no nodes at all",
			nodes: []corev1.Node{},
			network: &vlanmanv1.VlanNetwork{
				Spec: vlanmanv1.VlanNetworkSpec{
					ExcludedNodes: []string{},
				},
			},
			expectedError: true,
			errorContains: "There are no available nodes",
		},
		{
			name: "valid - excluded node not in cluster",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			network: &vlanmanv1.VlanNetwork{
				Spec: vlanmanv1.VlanNetworkSpec{
					ExcludedNodes: []string{"nonexistent-node"},
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{
				Nodes: tt.nodes,
			}

			err := validator.validateMinimumNodes(tt.network)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidator_validateUnique(t *testing.T) {
	existingNetworks := []vlanmanv1.VlanNetwork{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "network1"},
			Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 100},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "network2"},
			Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 200},
		},
	}

	tests := []struct {
		name          string
		newNetwork    *vlanmanv1.VlanNetwork
		expectedError bool
		errorContains string
	}{
		{
			name: "valid - unique VLAN ID",
			newNetwork: &vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "new-network"},
				Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 300},
			},
			expectedError: false,
		},
		{
			name: "invalid - duplicate VLAN ID",
			newNetwork: &vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "duplicate-network"},
				Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 100},
			},
			expectedError: true,
			errorContains: "There exists a network with that VLAN ID: network1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{
				Networks: existingNetworks,
			}

			err := validator.validateUnique(tt.newNetwork)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewCreationValidator(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = vlanmanv1.AddToScheme(scheme)

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
	}

	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
	}

	networks := []vlanmanv1.VlanNetwork{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-network"},
			Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 100},
		},
	}

	objs := make([]runtime.Object, 0)
	for _, node := range nodes {
		objs = append(objs, &node)
	}
	for _, pod := range pods {
		objs = append(objs, &pod)
	}
	for _, network := range networks {
		objs = append(objs, &network)
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()

	t.Run("valid creation", func(t *testing.T) {
		vlanNetwork := &vlanmanv1.VlanNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "test-network"},
			Spec: vlanmanv1.VlanNetworkSpec{
				VlanID:      200,
				GatewayIP:   "192.168.1.1",
				LocalSubnet: "192.168.1.0/24",
			},
		}

		objBytes, err := json.Marshal(vlanNetwork)
		require.NoError(t, err)

		validator, err := NewCreationValidator(client, context.Background(), objBytes)

		require.NoError(t, err)
		assert.NotNil(t, validator)
		assert.Equal(t, len(nodes), len(validator.Nodes))
		assert.Equal(t, len(pods), len(validator.Pods))
		assert.Equal(t, len(networks), len(validator.Networks))
		assert.Equal(t, "test-network", validator.NewNetwork.Name)
		assert.Equal(t, 200, validator.NewNetwork.Spec.VlanID)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		invalidJSON := []byte(`{"invalid": json}`)

		validator, err := NewCreationValidator(client, context.Background(), invalidJSON)

		assert.Error(t, err)
		assert.Nil(t, validator)
		assert.Contains(t, err.Error(), "Couldn't unmarshal vlan network for creation")
	})
}

func TestCreationValidator_Validate(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []corev1.Node
		networks      []vlanmanv1.VlanNetwork
		newNetwork    *vlanmanv1.VlanNetwork
		expectedError bool
		errorContains string
	}{
		{
			name: "valid network",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "existing"},
					Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 100},
				},
			},
			newNetwork: &vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "new"},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:        200,
					ExcludedNodes: []string{},
				},
			},
			expectedError: false,
		},
		{
			name: "invalid - no available nodes",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
			},
			networks: []vlanmanv1.VlanNetwork{},
			newNetwork: &vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "new"},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:        200,
					ExcludedNodes: []string{"node1"},
				},
			},
			expectedError: true,
			errorContains: "Couldn't validate minimum node requirement",
		},
		{
			name: "invalid - duplicate VLAN ID",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
			},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "existing"},
					Spec:       vlanmanv1.VlanNetworkSpec{VlanID: 100},
				},
			},
			newNetwork: &vlanmanv1.VlanNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "new"},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:        100, // Duplicate
					ExcludedNodes: []string{},
				},
			},
			expectedError: true,
			errorContains: "There exists a network with that VLAN ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &CreationValidator{
				Validator: &Validator{
					Nodes:    tt.nodes,
					Networks: tt.networks,
				},
				NewNetwork: tt.newNetwork,
			}

			err := validator.Validate()

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
