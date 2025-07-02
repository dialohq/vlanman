package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"
	errs "dialo.ai/vlanman/pkg/errors"
	"dialo.ai/vlanman/pkg/locker"
	u "dialo.ai/vlanman/pkg/utils"
	"github.com/alecthomas/repr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ http.Handler = (*MutatingWebhookHandler)(nil)

type MutatingWebhookHandler struct {
	Client client.Client
	Config rest.Config
	Env    controller.Envs
}

func (wh *MutatingWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r == nil {
		fmt.Println(&errs.UnrecoverableError{
			Context: "In MutatingWebhookHandler the request is nil",
			Err:     errs.ErrNilUnrecoverable,
		})
		return

	}

	in, err := u.ParseRequest(*r)
	if err != nil || in == nil {
		fmt.Println(&errs.UnrecoverableError{
			Context: "Parsing request in MutatingWebhookHandler",
			Err: &errs.ParsingError{
				Source: "Request",
				Err:    err,
			},
		})
		return
	}

	if in.Request.Kind.Kind != "Pod" {
		writeResponseNoPatch(w, in)
		return
	}

	pod := corev1.Pod{}
	err = json.Unmarshal(in.Request.Object.Raw, &pod)
	if err != nil {
		writeResponseDenied(w, in, (&errs.ParsingError{
			Source: "Unmarshaling raw object in mutating webhook",
			Err:    err,
		}).Error())
		return
	}

	networkName, existsNet := pod.Annotations[vlanmanv1.PodVlanmanNetworkAnnotation]
	poolName, existsPool := pod.Annotations[vlanmanv1.PodVlanmanIPPoolAnnotation]
	if !existsNet && !existsPool {
		writeResponseNoPatch(w, in)
		return
	}

	if !existsNet || !existsPool {
		writeResponseDenied(w, in, "Annotation missing")
	}

	dryRun := (in.Request != nil && in.Request.DryRun != nil && *in.Request.DryRun)
	clientSet, err := kubernetes.NewForConfig(&wh.Config)
	if err != nil || clientSet == nil {
		writeResponseDenied(w, in, (&errs.UnrecoverableError{
			Context: "Couldn't create a clientset from webhook.config in mutating webhook",
			Err:     err,
		}).Error())
		return
	}
	locker, err := locker.NewLeaseLocker(dryRun, *clientSet, vlanmanv1.LeaseName, wh.Env.NamespaceName)
	locker.Lock()

	network := &vlanmanv1.VlanNetwork{}
	err = wh.Client.Get(ctx, types.NamespacedName{Namespace: "", Name: networkName}, network)
	if err != nil {
		locker.Unlock()
		writeResponseDenied(w, in, (&errs.ClientRequestError{
			Action: "Get VlanNetwork",
			Err:    err,
		}).Error())
		return
	}

	network.Status = u.PopulateStatus(network.Status, poolName)
	pool := network.Status.FreeIPs[poolName]
	fmt.Println("Checking pool ", poolName, pool)
	pendingMap := network.Status.PendingIPs
	if pendingMap == nil {
		pendingMap = map[string][]string{}
	}
	pending := pendingMap[poolName]
	if pending == nil {
		pending = []string{}
	}

	found := false
	var assignedIP *string = nil
	for _, IP := range pool {
		if slices.Contains(pending, IP) {
			continue
		}
		found = true
		assignedIP = &IP
		pending = append(pending, IP)
		break
	}
	if !found || assignedIP == nil {
		locker.Unlock()
		writeResponseDenied(w, in,
			fmt.Sprintf("No free IP addresses found in pool %s for network %s",
				poolName,
				networkName,
			),
		)
		return
	}
	pendingMap[poolName] = pending
	network.Status.PendingIPs = pendingMap
	repr.Println(network.Status)
	err = wh.Client.Status().Update(ctx, network)
	if err != nil {
		writeResponseDenied(w, in,
			errs.NewClientRequestError(
				"Update status in mutating webhook",
				err,
			).Error(),
		)
		return
	}
	locker.Unlock()

	patch := []jsonPatch{}
	patch = preparePatch(pod,
		*network,
		wh.Env.WorkerInitImage,
		wh.Env.WorkerInitPullPolicy,
		*assignedIP,
	)
	resp, err := response(patch, in)
	if err != nil {
		writeResponseDenied(w, in, err.Error())
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(resp)
}

type patchOpts struct {
	Image       string
	PullPolicy  string
	NetworkName string
	AssignedIP  string
}

func preparePatch(pod corev1.Pod, network vlanmanv1.VlanNetwork, image, pullPolicy, IP string) []jsonPatch {
	address, subnet, found := strings.Cut(IP, "/")
	if !found {
		subnet = "32"
	}

	gwAddr, gwSubnet, found := strings.Cut(network.Spec.RemoteGatewayIP, "/")
	if !found {
		gwSubnet = "32"
	}

	patches := []jsonPatch{}

	// label
	// we have to replace the '/' in label because otherwise
	// json patch treats it like a path to patch. '~1' gets escaped to '/'
	labelKey := strings.Replace(vlanmanv1.WorkerPodLabelKey, "/", "~1", 1)
	if len(pod.Labels) != 0 {
		labelPath := "/metadata/labels/" + labelKey
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  labelPath,
			Value: network,
		})
	} else {
		labelPath := "/metadata/labels"
		patches = append(patches, jsonPatch{
			Op:   "add",
			Path: labelPath,
			Value: map[string]string{
				vlanmanv1.WorkerPodLabelKey: network.Name,
			},
		})
	}

	// init container
	containers := pod.Spec.InitContainers
	containerPath := "/spec/initContainers"
	initContainer := corev1.Container{
		Name:            vlanmanv1.WorkerInitContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullPolicy(pullPolicy),
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN"},
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "VLAN_NETWORK",
				Value: network.Name,
			},
			{
				Name:  "MACVLAN_IP",
				Value: address,
			},
			{
				Name:  "MACVLAN_SUBNET",
				Value: subnet,
			},
			{
				Name:  "REMOTE_ROUTES",
				Value: strings.Join(network.Spec.RemoteSubnet, ","),
			},
			{
				Name:  "GATEWAY_IP",
				Value: gwAddr,
			},
			{
				Name:  "GATEWAY_SUBNET",
				Value: gwSubnet,
			},
		},
	}
	// we want vlan to run ideally first since other init containers might
	// want to use the vlan connection. But the order in which mutating webhooks
	// are called is non deterministic so this is the best we can do ;(
	containers = append([]corev1.Container{initContainer}, containers...)
	patches = append(patches, jsonPatch{
		Op:    "replace",
		Path:  containerPath,
		Value: containers,
	})
	// env
	for idx, container := range pod.Spec.Containers {
		envs := container.Env
		envs = append(envs, []corev1.EnvVar{
			{
				Name:  "VLAN_IP",
				Value: address,
			},
			{
				Name:  "VLAN_SUBNET",
				Value: subnet,
			},
		}...)

		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/containers/%d/env", idx),
			Value: envs,
		})
	}
	return patches
}

func response(patch []jsonPatch, in *admissionv1.AdmissionReview) ([]byte, error) {
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil, &errs.ParsingError{
			Source: "Marshaling json patch in mutating webhook response creation",
			Err:    err,
		}
	}
	resp := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:       in.Request.UID,
			Allowed:   true,
			Patch:     patchJSON,
			PatchType: u.Ptr(admissionv1.PatchTypeJSONPatch),
		},
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		return nil, &errs.ParsingError{
			Source: "Marshaling response in mutating webhook response creation",
			Err:    err,
		}
	}
	return respJSON, nil
}

type jsonPatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}
