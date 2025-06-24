package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"
)

func TestGetAction(t *testing.T) {
	tests := []struct {
		name     string
		req      *admissionv1.AdmissionRequest
		expected any
	}{
		{
			name: "creation action",
			req: &admissionv1.AdmissionRequest{
				OldObject: runtime.RawExtension{Raw: nil},
				Object:    runtime.RawExtension{Raw: []byte(`{"test": "data"}`)},
			},
			expected: creationAction{},
		},
		{
			name: "deletion action",
			req: &admissionv1.AdmissionRequest{
				OldObject: runtime.RawExtension{Raw: []byte(`{"test": "data"}`)},
				Object:    runtime.RawExtension{Raw: nil},
			},
			expected: deletionAction{},
		},
		{
			name: "update action",
			req: &admissionv1.AdmissionRequest{
				OldObject: runtime.RawExtension{Raw: []byte(`{"test": "old"}`)},
				Object:    runtime.RawExtension{Raw: []byte(`{"test": "new"}`)},
			},
			expected: updateAction{},
		},
		{
			name: "unknown action",
			req: &admissionv1.AdmissionRequest{
				OldObject: runtime.RawExtension{Raw: nil},
				Object:    runtime.RawExtension{Raw: nil},
			},
			expected: unknownAction{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAction(tt.req)
			assert.IsType(t, tt.expected, result)
		})
	}
}

func TestNoPatchResponse(t *testing.T) {
	uid := types.UID("test-uid")
	input := &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: uid,
		},
	}

	result := noPatchResponse(input)

	assert.Equal(t, "AdmissionReview", result.Kind)
	assert.Equal(t, "admission.k8s.io/v1", result.APIVersion)
	assert.Equal(t, uid, result.Response.UID)
	assert.True(t, result.Response.Allowed)
}

func TestDeniedResponse(t *testing.T) {
	uid := types.UID("test-uid")
	input := &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: uid,
		},
	}

	t.Run("with reason", func(t *testing.T) {
		reason := "test reason"
		result := deniedResponse(input, reason)

		assert.Equal(t, "AdmissionReview", result.Kind)
		assert.Equal(t, "admission.k8s.io/v1", result.APIVersion)
		assert.Equal(t, uid, result.Response.UID)
		assert.False(t, result.Response.Allowed)
		assert.Equal(t, reason, result.Response.Result.Message)
	})

	t.Run("without reason", func(t *testing.T) {
		result := deniedResponse(input)

		assert.Equal(t, "No reason provided.", result.Response.Result.Message)
	})
}

func TestValidatingWebhookHandler_ServeHTTP(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = vlanmanv1.AddToScheme(scheme)

	// Create test nodes
	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node2"},
		},
	}

	// Create test VlanNetworks
	existingNetworks := []vlanmanv1.VlanNetwork{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-network"},
			Spec: vlanmanv1.VlanNetworkSpec{
				VlanID: 100,
			},
		},
	}

	// Create fake client with test data
	objs := make([]runtime.Object, 0)
	for _, node := range nodes {
		objs = append(objs, &node)
	}
	for _, network := range existingNetworks {
		objs = append(objs, &network)
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()

	handler := &ValidatingWebhookHandler{
		Client: client,
		Env:    controller.Envs{},
	}

	t.Run("valid creation", func(t *testing.T) {
		// Create a valid VlanNetwork
		vlanNetwork := &vlanmanv1.VlanNetwork{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VlanNetwork",
				APIVersion: "vlanman.dialo.ai/v1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "test-network"},
			Spec: vlanmanv1.VlanNetworkSpec{
				VlanID:        200,
				GatewayIP:     "192.168.1.1",
				LocalSubnet:   "192.168.1.0/24",
				RemoteSubnet:  "192.168.2.0/24",
				ExcludedNodes: []string{},
			},
		}

		objBytes, err := json.Marshal(vlanNetwork)
		require.NoError(t, err)

		admissionReview := &admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AdmissionReview",
				APIVersion: "admission.k8s.io/v1",
			},
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("test-uid"),
				Object: runtime.RawExtension{
					Raw: objBytes,
				},
			},
		}

		reqBytes, err := json.Marshal(admissionReview)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response admissionv1.AdmissionReview
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response.Response.Allowed)
	})

	t.Run("duplicate VLAN ID", func(t *testing.T) {
		// Create a VlanNetwork with duplicate VLAN ID
		vlanNetwork := &vlanmanv1.VlanNetwork{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VlanNetwork",
				APIVersion: "vlanman.dialo.ai/v1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "duplicate-network"},
			Spec: vlanmanv1.VlanNetworkSpec{
				VlanID:        100, // Same as existing network
				GatewayIP:     "192.168.1.1",
				LocalSubnet:   "192.168.1.0/24",
				RemoteSubnet:  "192.168.2.0/24",
				ExcludedNodes: []string{},
			},
		}

		objBytes, err := json.Marshal(vlanNetwork)
		require.NoError(t, err)

		admissionReview := &admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AdmissionReview",
				APIVersion: "admission.k8s.io/v1",
			},
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("test-uid"),
				Object: runtime.RawExtension{
					Raw: objBytes,
				},
			},
		}

		reqBytes, err := json.Marshal(admissionReview)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response admissionv1.AdmissionReview
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.False(t, response.Response.Allowed)
		assert.Contains(t, response.Response.Result.Message, "There exists a network with that VLAN ID")
	})

	t.Run("all nodes excluded", func(t *testing.T) {
		// Create a VlanNetwork that excludes all nodes
		vlanNetwork := &vlanmanv1.VlanNetwork{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VlanNetwork",
				APIVersion: "vlanman.dialo.ai/v1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "no-nodes-network"},
			Spec: vlanmanv1.VlanNetworkSpec{
				VlanID:        300,
				GatewayIP:     "192.168.1.1",
				LocalSubnet:   "192.168.1.0/24",
				RemoteSubnet:  "192.168.2.0/24",
				ExcludedNodes: []string{"node1", "node2"}, // Exclude all nodes
			},
		}

		objBytes, err := json.Marshal(vlanNetwork)
		require.NoError(t, err)

		admissionReview := &admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AdmissionReview",
				APIVersion: "admission.k8s.io/v1",
			},
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("test-uid"),
				Object: runtime.RawExtension{
					Raw: objBytes,
				},
			},
		}

		reqBytes, err := json.Marshal(admissionReview)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response admissionv1.AdmissionReview
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.False(t, response.Response.Allowed)
		assert.Contains(t, response.Response.Result.Message, "There are no available nodes")
	})
}
