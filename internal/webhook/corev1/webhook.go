package corev1

import (
	"context"
	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"
	errs "dialo.ai/vlanman/pkg/errors"
	"dialo.ai/vlanman/pkg/locker"
	u "dialo.ai/vlanman/pkg/utils"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/client-go/rest"
	"maps"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"slices"
	"strings"
	"time"
)

func SetupVlanmanWebhookWithManager(mgr ctrl.Manager, e controller.Envs) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithDefaulter(&VlanmanPodCustomDefaulter{
			Client: mgr.GetClient(),
			Config: *mgr.GetConfig(),
			Env:    e,
		}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=NoneOnDryRun,groups="",resources=pods,verbs=create,versions=v1,name=webhook.vlanman.dialo.ai,admissionReviewVersions=v1,serviceName=replaceme[.Values.webhook.serviceName],servicePort=443,serviceNamespace=replaceme[.Values.global.namespace]

type VlanmanPodCustomDefaulter struct {
	Client client.Client
	Config rest.Config
	Env    controller.Envs
}

var _ webhook.CustomDefaulter = &VlanmanPodCustomDefaulter{}

func (v *VlanmanPodCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return errs.NewTypeMismatchError("Mutating webhook", obj)
	}

	networkName, existsNet := pod.Annotations[vlanmanv1.PodVlanmanNetworkAnnotation]
	poolName, existsPool := pod.Annotations[vlanmanv1.PodVlanmanIPPoolAnnotation]
	if !existsNet && !existsPool {
		return nil
	}

	if !existsNet || !existsPool {
		return &errs.MissingAnnotationError{Resource: fmt.Sprintf("%s@%s", pod.Name, pod.Namespace)}
	}

	clientSet, err := kubernetes.NewForConfig(&v.Config)
	if err != nil || clientSet == nil {
		return &errs.UnrecoverableError{
			Context: "Couldn't create a clientset from webhook.config in mutating webhook",
			Err:     err,
		}
	}
	locker, err := locker.NewLeaseLocker(false, *clientSet, vlanmanv1.LeaseName, v.Env.NamespaceName)
	locker.Lock()

	network := &vlanmanv1.VlanNetwork{}
	err = v.Client.Get(ctx, types.NamespacedName{Namespace: "", Name: networkName}, network)
	if err != nil {
		locker.Unlock()
		return &errs.ClientRequestError{
			Action: "Get VlanNetwork",
			Err:    err,
		}
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
		return &errs.NoIPInPoolError{
			Resource: fmt.Sprintf("%s@%s", pod.Name, pod.Namespace),
		}
	}

	network.Status.PendingIPs[poolName] = pendingMap
	err = v.Client.Status().Update(ctx, network)
	if err != nil {
		return errs.NewClientRequestError(
			"Update status in mutating webhook",
			err,
		)
	}
	locker.Unlock()

	managers := corev1.PodList{}
	requirement, err := labels.NewRequirement(vlanmanv1.ManagerSetLabelKey, "==", []string{network.Name})
	if err != nil {
		return &errs.InternalError{
			Context: fmt.Sprintf("mutating webhook list manager pods requirement fails to compile: %s", err.Error()),
		}
	}
	selector := labels.NewSelector().Add(*requirement)
	err = v.Client.List(ctx, &managers, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return errs.NewClientRequestError("Listing manager pods in mutating webhook", err)
	}
	if len(managers.Items) == 0 {
		return &errs.NoManagerPodsError{
			Resource: fmt.Sprintf("%s@%s", pod.Name, pod.Namespace),
			Network:  network.Name,
		}
	}
	endpoints := map[string]string{}
	for _, man := range managers.Items {
		if man.Status.PodIP == "" {
			return &errs.ManagerNotReadyError{
				Resource: fmt.Sprintf("%s@%s", pod.Name, pod.Namespace),
				Manager:  fmt.Sprintf("%s@%s", man.Name, man.Namespace),
			}
		}
		endpoints[man.Spec.NodeName] = man.Status.PodIP
	}

	applyPatch(pod, *network, v.Env.WorkerInitImage, v.Env.WorkerInitPullPolicy, *assignedIP, endpoints)
	return nil
}

func applyPatch(pod *corev1.Pod, network vlanmanv1.VlanNetwork, image, pullPolicy, IP string, endpoints map[string]string) {
	address, subnet, found := strings.Cut(IP, "/")
	if !found {
		subnet = "32"
	}

	gwAddr, gwSubnet, found := strings.Cut(network.Spec.RemoteGatewayIP, "/")
	if !found {
		gwSubnet = "32"
	}

	if len(pod.Labels) != 0 {
		pod.Labels[vlanmanv1.WorkerPodLabelKey] = network.Name
	} else {
		pod.Labels = map[string]string{
			vlanmanv1.WorkerPodLabelKey: network.Name,
		}
	}

	managers := []string{}
	for k, v := range endpoints {
		managers = append(managers, fmt.Sprintf("%s=%s", k, v))
	}

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
	pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)

	// env
	for idx := range pod.Spec.Containers {
		pod.Spec.Containers[idx].Env = append(pod.Spec.Containers[idx].Env, []corev1.EnvVar{
			{
				Name:  "VLAN_IP",
				Value: address,
			},
			{
				Name:  "VLAN_SUBNET",
				Value: subnet,
			},
		}...)

	}

	if network.Spec.ManagerAffinity != nil {
		pod.Spec.Affinity = mergeAffinity(pod.Spec.Affinity, network.Spec.ManagerAffinity)
	}
}

func mergeAffinity(base, override *corev1.Affinity) *corev1.Affinity {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	merged := &corev1.Affinity{}

	if base.NodeAffinity == nil {
		merged.NodeAffinity = override.NodeAffinity
	} else if override.NodeAffinity == nil {
		merged.NodeAffinity = base.NodeAffinity
	} else {
		merged.NodeAffinity = override.NodeAffinity
	}

	if base.PodAffinity == nil {
		merged.PodAffinity = override.PodAffinity
	} else if override.PodAffinity == nil {
		merged.PodAffinity = base.PodAffinity
	} else {
		merged.PodAffinity = override.PodAffinity
	}

	if base.PodAntiAffinity == nil {
		merged.PodAntiAffinity = override.PodAntiAffinity
	} else if override.PodAntiAffinity == nil {
		merged.PodAntiAffinity = base.PodAntiAffinity
	} else {
		merged.PodAntiAffinity = override.PodAntiAffinity
	}

	return merged
}
