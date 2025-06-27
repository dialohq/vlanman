package controller

import (
	"strconv"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ManagerSet struct {
	OwnerNetworkName string
	VlanID           int64
	ExcludedNodes    []string
}

func managerCmp(a, b ManagerSet) int {
	c := strings.Compare(a.OwnerNetworkName, b.OwnerNetworkName)
	if c == 0 {
		if a.VlanID < b.VlanID {
			return -1
		} else if a.VlanID > b.VlanID {
			return 1
		}
		return 0
	}
	return c
}

func daemonSetFromManager(mgr ManagerSet, e Envs) appsv1.DaemonSet {
	spec := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{vlanmanv1.ManagerSetNamePrefix, mgr.OwnerNetworkName}, "-"),
			Namespace: e.NamespaceName,
			Labels: map[string]string{
				vlanmanv1.ManagerSetLabelKey: mgr.OwnerNetworkName,
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					vlanmanv1.ManagerSetLabelKey: mgr.OwnerNetworkName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						vlanmanv1.ManagerSetLabelKey: mgr.OwnerNetworkName,
					},
				},
				Spec: corev1.PodSpec{
					HostPID: true,
					Containers: []corev1.Container{
						{
							Name:            vlanmanv1.ManagerContainerName,
							Image:           e.VlanManagerImage,
							ImagePullPolicy: getPullPolicy(e.VlanManagerPullPolicy),
							Env: []corev1.EnvVar{
								{
									Name:  "VLAN_ID",
									Value: strconv.FormatInt(mgr.VlanID, 10),
								},
							},
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
			},
		},
	}
	terms := []corev1.NodeSelectorTerm{}
	for _, node := range mgr.ExcludedNodes {
		terms = append(terms, corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      "kubernetes.io/hostname",
					Operator: corev1.NodeSelectorOpNotIn,
					Values:   []string{node},
				},
			},
		})
	}
	if len(terms) != 0 {
		spec.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: terms,
				},
			},
		}
	}
	return spec
}

func managerFromSet(d appsv1.DaemonSet) ManagerSet {
	excludedNodes := []string{}
	if d.Spec.Template.Spec.Affinity != nil &&
		d.Spec.Template.Spec.Affinity.NodeAffinity != nil &&
		d.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil &&
		d.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms != nil {

		for _, t := range d.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			if len(t.MatchExpressions) != 0 {
				excludedNodes = append(excludedNodes, t.MatchExpressions[0].Values...)
			}
		}
	}

	val, _ := strconv.ParseInt(d.Spec.Template.Spec.Containers[0].Env[0].Value, 10, 64)
	return ManagerSet{
		OwnerNetworkName: d.Labels[vlanmanv1.ManagerSetLabelKey],
		VlanID:           val,
		ExcludedNodes:    excludedNodes,
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

func createDesiredManagerSet(network vlanmanv1.VlanNetwork) ManagerSet {
	return ManagerSet{
		OwnerNetworkName: network.Name,
		VlanID:           int64(network.Spec.VlanID),
		ExcludedNodes:    network.Spec.ExcludedNodes,
	}
}
