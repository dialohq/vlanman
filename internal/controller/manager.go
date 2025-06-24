package controller

import (
	vlanmanv1 "dialo.ai/vlanman/api/v1"
	corev1 "k8s.io/api/core/v1"
)

type ManagerPod struct {
	Exists bool
}

func managerFromPod(pod corev1.Pod) ManagerPod {
	return ManagerPod{Exists: true}
}

func createDesiredManager(network vlanmanv1.VlanNetwork) ManagerPod {
	return ManagerPod{Exists: true}
}
