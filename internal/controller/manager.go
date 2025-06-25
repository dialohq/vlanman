package controller

import (
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ManagerPod struct {
	Exists           bool
	OwnerNetworkName string
	NodeName         string
}

func managerFromPod(pod corev1.Pod) ManagerPod {
	return ManagerPod{
		Exists:           true,
		OwnerNetworkName: pod.Labels[vlanmanv1.ManagerPodLabelKey],
		NodeName:         pod.Spec.NodeName,
	}
}

func getPullPolicy(pp string) corev1.PullPolicy {
	switch pp {
	case "Always":
		return corev1.PullAlways
	case "Never":
		return corev1.PullNever
	case "IfNotPresent":
		return corev1.PullIfNotPresent
	default:
		return corev1.PullIfNotPresent
	}
}

func podFromManager(mgr ManagerPod, e Envs) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{vlanmanv1.ManagerPodNamePrefix, mgr.OwnerNetworkName}, "-"),
			Namespace: e.NamespaceName,
			Labels: map[string]string{
				vlanmanv1.ManagerPodLabelKey: mgr.OwnerNetworkName,
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				vlanmanv1.NodeSelectorHostName: mgr.NodeName,
			},
			Containers: []corev1.Container{
				{
					Name:            vlanmanv1.ManagerContainerName,
					Image:           e.VlanManagerImage,
					ImagePullPolicy: getPullPolicy(e.VlanManagerPullPolicy),
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{
								"NET_ADMIN",
								"NET_RAW",
							},
						},
					},
				},
			},
		},
	}
}

func createDesiredManager(network vlanmanv1.VlanNetwork, nodeName string) ManagerPod {
	return ManagerPod{
		Exists:           true,
		OwnerNetworkName: network.Name,
		NodeName:         nodeName,
	}
}
