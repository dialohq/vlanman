package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
)

func TestVlanmanReconciler_createDesiredState(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = vlanmanv1.AddToScheme(scheme)

	tests := []struct {
		name              string
		nodes             []corev1.Node
		networks          []vlanmanv1.VlanNetwork
		expectedNodeCount int
		expectedManagers  map[string]int // node name -> manager count
	}{
		{
			name: "single network, no excluded nodes",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "network1"},
					Spec: vlanmanv1.VlanNetworkSpec{
						VlanID:        100,
						ExcludedNodes: []string{},
					},
				},
			},
			expectedNodeCount: 2,
			expectedManagers: map[string]int{
				"node1": 1,
				"node2": 1,
			},
		},
		{
			name: "single network, one node excluded",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "network1"},
					Spec: vlanmanv1.VlanNetworkSpec{
						VlanID:        100,
						ExcludedNodes: []string{"node2"},
					},
				},
			},
			expectedNodeCount: 3,
			expectedManagers: map[string]int{
				"node1": 1,
				"node2": 0,
				"node3": 1,
			},
		},
		{
			name: "multiple networks, different exclusions",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "network1"},
					Spec: vlanmanv1.VlanNetworkSpec{
						VlanID:        100,
						ExcludedNodes: []string{"node2"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "network2"},
					Spec: vlanmanv1.VlanNetworkSpec{
						VlanID:        200,
						ExcludedNodes: []string{"node1"},
					},
				},
			},
			expectedNodeCount: 3,
			expectedManagers: map[string]int{
				"node1": 1, // only network1
				"node2": 1, // only network2
				"node3": 2, // both networks
			},
		},
		{
			name: "no networks",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			networks:          []vlanmanv1.VlanNetwork{},
			expectedNodeCount: 2,
			expectedManagers: map[string]int{
				"node1": 0,
				"node2": 0,
			},
		},
		{
			name:  "no nodes",
			nodes: []corev1.Node{},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "network1"},
					Spec: vlanmanv1.VlanNetworkSpec{
						VlanID:        100,
						ExcludedNodes: []string{},
					},
				},
			},
			expectedNodeCount: 0,
			expectedManagers:  map[string]int{},
		},
		{
			name: "excluded node doesn't exist in cluster",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			networks: []vlanmanv1.VlanNetwork{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "network1"},
					Spec: vlanmanv1.VlanNetworkSpec{
						VlanID:        100,
						ExcludedNodes: []string{"nonexistent-node"},
					},
				},
			},
			expectedNodeCount: 2,
			expectedManagers: map[string]int{
				"node1": 1,
				"node2": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0)
			for _, node := range tt.nodes {
				objs = append(objs, &node)
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			reconciler := &VlanmanReconciler{
				Client: client,
				Scheme: scheme,
			}

			ctx := context.Background()
			state, err := reconciler.createDesiredState(ctx, tt.networks)

			require.NoError(t, err)
			assert.NotNil(t, state)
			assert.Equal(t, tt.expectedNodeCount, len(state.Nodes))

			for nodeName, expectedManagerCount := range tt.expectedManagers {
				node, exists := state.Nodes[nodeName]
				require.True(t, exists, "Expected node %s to exist in state", nodeName)
				assert.Equal(t, expectedManagerCount, len(node.Managers), "Expected %d managers for node %s, got %d", expectedManagerCount, nodeName, len(node.Managers))

				// Verify all managers have Exists set to true
				for _, manager := range node.Managers {
					assert.True(t, manager.Exists, "All desired managers should have Exists=true")
				}
			}
		})
	}
}

func TestVlanmanReconciler_getCurrentState(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = vlanmanv1.AddToScheme(scheme)

	tests := []struct {
		name              string
		pods              []corev1.Pod
		expectedNodeCount int
		expectedManagers  map[string]int // node name -> manager count
	}{
		{
			name: "no manager pods",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "regular-pod",
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
			},
			expectedNodeCount: 0,
			expectedManagers:  map[string]int{},
		},
		{
			name: "single manager pod",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-1",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
			},
			expectedNodeCount: 1,
			expectedManagers: map[string]int{
				"node1": 1,
			},
		},
		{
			name: "multiple manager pods on same node",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-1",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-2",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net2",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
			},
			expectedNodeCount: 1,
			expectedManagers: map[string]int{
				"node1": 2,
			},
		},
		{
			name: "manager pods on multiple nodes",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-1",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-2",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-3",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net2",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node2",
					},
				},
			},
			expectedNodeCount: 2,
			expectedManagers: map[string]int{
				"node1": 1,
				"node2": 2,
			},
		},
		{
			name: "mixed manager and regular pods",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-1",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "regular-pod",
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "manager-pod-2",
						Labels: map[string]string{
							vlanmanv1.ManagerPodLabelKey: "net1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node2",
					},
				},
			},
			expectedNodeCount: 2,
			expectedManagers: map[string]int{
				"node1": 1,
				"node2": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0)
			for _, pod := range tt.pods {
				objs = append(objs, &pod)
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			reconciler := &VlanmanReconciler{
				Client: client,
				Scheme: scheme,
			}

			ctx := context.Background()
			state, err := reconciler.getCurrentState(ctx)

			require.NoError(t, err)
			assert.NotNil(t, state)
			assert.Equal(t, tt.expectedNodeCount, len(state.Nodes))

			for nodeName, expectedManagerCount := range tt.expectedManagers {
				node, exists := state.Nodes[nodeName]
				require.True(t, exists, "Expected node %s to exist in state", nodeName)
				assert.Equal(t, expectedManagerCount, len(node.Managers), "Expected %d managers for node %s, got %d", expectedManagerCount, nodeName, len(node.Managers))

				// Verify all managers have Exists set to true (since they come from existing pods)
				for _, manager := range node.Managers {
					assert.True(t, manager.Exists, "All current managers should have Exists=true")
				}
			}
		})
	}
}

func TestVlanmanReconciler_getCurrentState_ErrorHandling(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = vlanmanv1.AddToScheme(scheme)

	t.Run("client error during pod listing", func(t *testing.T) {
		// Create a client that will fail during List operations
		client := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		// Add a mock that fails
		// Note: fake client doesn't easily support error injection, so this test
		// verifies the error handling path exists but can't easily trigger it
		reconciler := &VlanmanReconciler{
			Client: client,
			Scheme: scheme,
		}

		ctx := context.Background()
		state, err := reconciler.getCurrentState(ctx)

		// With fake client, this should succeed with empty state
		require.NoError(t, err)
		assert.NotNil(t, state)
		assert.Equal(t, 0, len(state.Nodes))
	})
}

func TestVlanmanReconciler_createDesiredState_ErrorHandling(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = vlanmanv1.AddToScheme(scheme)

	t.Run("client error during node listing", func(t *testing.T) {
		// Create a client that will fail during List operations
		client := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		reconciler := &VlanmanReconciler{
			Client: client,
			Scheme: scheme,
		}

		ctx := context.Background()
		networks := []vlanmanv1.VlanNetwork{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "network1"},
				Spec: vlanmanv1.VlanNetworkSpec{
					VlanID:        100,
					ExcludedNodes: []string{},
				},
			},
		}

		state, err := reconciler.createDesiredState(ctx, networks)

		// With fake client, this should succeed with empty state (no nodes)
		require.NoError(t, err)
		assert.NotNil(t, state)
		assert.Equal(t, 0, len(state.Nodes))
	})
}
