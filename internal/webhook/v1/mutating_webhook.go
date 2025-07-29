package v1

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"
	errs "dialo.ai/vlanman/pkg/errors"
	"dialo.ai/vlanman/pkg/locker"
	u "dialo.ai/vlanman/pkg/utils"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ http.Handler = (*MutatingWebhookHandler)(nil)

type MutatingWebhookHandler struct {
	Client client.Client
	Config rest.Config
	Env    controller.Envs
}

func (wh *MutatingWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := log.FromContext(ctx)

	if r == nil {
		log.Error(&errs.UnrecoverableError{
			Context: "In MutatingWebhookHandler the request is nil",
			Err:     errs.ErrNilUnrecoverable,
		}, "Mutating webhook error")
		return
	}

	in, err := u.ParseRequest(*r)
	if err != nil || in == nil {
		log.Error(&errs.UnrecoverableError{
			Context: "Parsing request in MutatingWebhookHandler",
			Err: &errs.ParsingError{
				Source: "Request",
				Err:    err,
			},
		}, "Mutating webhook error")
		return
	}

	if in.Request.Kind.Kind != "Pod" {
		writeResponseNoPatch(ctx, w, in)
		return
	}

	pod := corev1.Pod{}
	err = json.Unmarshal(in.Request.Object.Raw, &pod)
	if err != nil {
		writeResponseDenied(ctx, w, in, (&errs.ParsingError{
			Source: "Unmarshaling raw object in mutating webhook",
			Err:    err,
		}).Error())
		return
	}

	networkName, existsNet := pod.Annotations[vlanmanv1.PodVlanmanNetworkAnnotation]
	poolName, existsPool := pod.Annotations[vlanmanv1.PodVlanmanIPPoolAnnotation]
	if !existsNet && !existsPool {
		writeResponseNoPatch(ctx, w, in)
		return
	}

	if !existsNet || !existsPool {
		writeResponseDenied(ctx, w, in, "Annotation missing")
		return
	}

	dryRun := (in.Request != nil && in.Request.DryRun != nil && *in.Request.DryRun)
	clientSet, err := kubernetes.NewForConfig(&wh.Config)
	if err != nil || clientSet == nil {
		writeResponseDenied(ctx, w, in, (&errs.UnrecoverableError{
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
		writeResponseDenied(ctx, w, in, (&errs.ClientRequestError{
			Action: "Get VlanNetwork",
			Err:    err,
		}).Error())
		return
	}

	network.Status = u.PopulateStatus(network.Status, poolName)
	pool := network.Status.FreeIPs[poolName]
	if network.Status.PendingIPs == nil {
		network.Status.PendingIPs = map[string]map[string]string{}
	}

	pendingMap := network.Status.PendingIPs[poolName]
	if pendingMap == nil {
		pendingMap = map[string]string{}
	}

	found := false
	var assignedIP *string = nil
	for _, IP := range pool {
		if slices.Contains(slices.Collect(maps.Values(pendingMap)), IP) {
			continue
		}
		found = true
		assignedIP = &IP
		pendingMap[IP] = time.Now().Format(time.Layout)
		break
	}
	if !found || assignedIP == nil {
		locker.Unlock()
		writeResponseDenied(ctx, w, in,
			fmt.Sprintf("No free IP addresses found in pool %s for network %s",
				poolName,
				networkName,
			),
		)
		return
	}

	network.Status.PendingIPs[poolName] = pendingMap
	err = wh.Client.Status().Update(ctx, network)
	if err != nil {
		writeResponseDenied(ctx, w, in,
			errs.NewClientRequestError(
				"Update status in mutating webhook",
				err,
			).Error(),
		)
		return
	}
	locker.Unlock()

	managers := corev1.PodList{}
	requirement, err := labels.NewRequirement(vlanmanv1.ManagerSetLabelKey, "==", []string{network.Name})
	if err != nil {
		err = &errs.InternalError{
			Context: fmt.Sprintf("mutating webhook list manager pods requirement fails to compile: %s", err.Error()),
		}
		writeResponseDenied(ctx, w, in, err.Error())
		return
	}
	selector := labels.NewSelector().Add(*requirement)
	err = wh.Client.List(ctx, &managers, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		err = errs.NewClientRequestError("Listing manager pods in mutating webhook", err)
		log.Error(err, "Couldn't request list manager pods")
		writeResponseDenied(ctx, w, in, err.Error())
		if err != nil {
			log.Error(err, "Validating webhook error")
		}
		return
	}
	if len(managers.Items) == 0 {
		writeResponseDenied(ctx, w, in, "There are no manager pods matching this network")
		return
	}
	endpoints := map[string]string{}
	for _, man := range managers.Items {
		if man.Status.PodIP == "" {
			writeResponseDenied(ctx, w, in, fmt.Sprintf("Manager %s is not ready yet", man.Name))
			return
		}
		endpoints[man.Spec.NodeName] = man.Status.PodIP
	}

	patch := []jsonPatch{}
	patch = preparePatch(pod,
		*network,
		wh.Env.WorkerInitImage,
		wh.Env.WorkerInitPullPolicy,
		*assignedIP,
		endpoints,
	)
	resp, err := response(patch, in)
	if err != nil {
		writeResponseDenied(ctx, w, in, err.Error())
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

func preparePatch(pod corev1.Pod, network vlanmanv1.VlanNetwork, image, pullPolicy, IP string, endpoints map[string]string) []jsonPatch {
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
			Value: network.Name,
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
	managers := []string{}
	for k, v := range endpoints {
		managers = append(managers, fmt.Sprintf("%s=%s", k, v))
	}

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
			{
				Name:  "MANAGERS",
				Value: strings.Join(managers, ","),
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

	if len(network.Spec.ExcludedNodes) != 0 {
		patches = append(patches, nodeAffinityPatch(pod, network.Spec.ExcludedNodes))
	}
	return patches
}

func nodeAffinityPatch(pod corev1.Pod, excludedNodes []string) jsonPatch {
	// Build a complete affinity structure
	affinity := pod.Spec.Affinity
	var nodeSelectorTerms []any

	// Reuse existing nodeSelectorTerms if present
	if affinity != nil &&
		affinity.NodeAffinity != nil &&
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		for _, term := range affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			nodeSelectorTerms = append(nodeSelectorTerms, term)
		}
	}

	// Append a new term
	newTerm := map[string]any{
		"matchExpressions": []map[string]any{
			{
				"key":      "kubernetes.io/hostname",
				"operator": "NotIn",
				"values":   excludedNodes,
			},
		},
	}
	nodeSelectorTerms = append(nodeSelectorTerms, newTerm)

	// Construct the full affinity value
	affinityValue := map[string]any{
		"nodeAffinity": map[string]any{
			"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
				"nodeSelectorTerms": nodeSelectorTerms,
			},
		},
	}

	return jsonPatch{
		Op:    "add",
		Path:  "/spec/affinity",
		Value: affinityValue,
	}
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
