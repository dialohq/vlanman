package controller

import (
	"testing"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInterfaceFromDaemon(t *testing.T) {
	tests := []struct {
		name        string
		pod         corev1.Pod
		pid         int
		id          int
		ttl         *int32
		image       string
		networkName string
		pullPolicy  string
		expectedJob func() string // returns expected job name
	}{
		{
			name: "basic interface job creation",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
			},
			pid:         12345,
			id:          100,
			ttl:         func() *int32 { t := int32(3600); return &t }(),
			image:       "vlanman-interface:latest",
			networkName: "test-network",
			pullPolicy:  "IfNotPresent",
			expectedJob: func() string { return "create-vlan-job-test-network-test-node" },
		},
		{
			name: "interface job with nil TTL",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-2",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "worker-node-1",
				},
			},
			pid:         67890,
			id:          200,
			ttl:         nil,
			image:       "vlanman-interface:v1.0.0",
			networkName: "production-network",
			pullPolicy:  "Always",
			expectedJob: func() string { return "create-vlan-job-production-network-worker-node-1" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := interfaceFromDaemon(tt.pod, tt.pid, tt.id, tt.ttl, tt.image, tt.networkName, tt.pullPolicy, []vlanmanv1.IPMapping{})

			// Verify job metadata
			assert.Equal(t, tt.expectedJob(), job.Name)
			assert.Equal(t, tt.pod.Namespace, job.Namespace)

			// Verify job spec
			assert.Equal(t, tt.ttl, job.Spec.TTLSecondsAfterFinished)
			assert.True(t, job.Spec.Template.Spec.HostNetwork)
			assert.True(t, job.Spec.Template.Spec.HostPID)
			assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)

			// Verify node selector
			expectedNodeSelector := map[string]string{
				"kubernetes.io/hostname": tt.pod.Spec.NodeName,
			}
			assert.Equal(t, expectedNodeSelector, job.Spec.Template.Spec.NodeSelector)

			// Verify container configuration
			containers := job.Spec.Template.Spec.Containers
			assert.Len(t, containers, 1)

			container := containers[0]
			assert.Equal(t, "create-vlan", container.Name)
			assert.Equal(t, tt.image, container.Image)
			assert.Equal(t, corev1.PullPolicy(tt.pullPolicy), container.ImagePullPolicy)

			// Verify environment variables
			expectedEnvVars := []corev1.EnvVar{
				{Name: "PID", Value: "12345"},
				{Name: "ID", Value: "100"},
				{Name: "INTERFACE", Value: ""},
			}
			if tt.pid == 67890 {
				expectedEnvVars = []corev1.EnvVar{
					{Name: "PID", Value: "67890"},
					{Name: "ID", Value: "200"},
					{Name: "INTERFACE", Value: ""},
				}

			}
			assert.Equal(t, expectedEnvVars, container.Env)

			// Verify security context
			assert.NotNil(t, container.SecurityContext)
			assert.NotNil(t, container.SecurityContext.Capabilities)
			expectedCapabilities := []corev1.Capability{"NET_ADMIN", "NET_RAW"}
			assert.Equal(t, expectedCapabilities, container.SecurityContext.Capabilities.Add)
		})
	}
}
