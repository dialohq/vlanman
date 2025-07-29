package controller

import (
	"strconv"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	u "dialo.ai/vlanman/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type ManagerSet struct {
	OwnerNetworkName string
	VlanID           int64
	GatewayIP        string
	GatewaySubnet    int64
	LocalRoutes      []string
	RemoteRoutes     []string
	ExcludedNodes    []string
	Mappings         []vlanmanv1.IPMapping
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
					ServiceAccountName: e.ServiceAccountName,
					HostPID:            true,
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{
									Name:          vlanmanv1.ManagerPodAPIPortName,
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: vlanmanv1.ManagerPodAPIPort,
								},
							},
							Name:            vlanmanv1.ManagerContainerName,
							Image:           e.VlanManagerImage,
							ImagePullPolicy: getPullPolicy(e.VlanManagerPullPolicy),
							Env: []corev1.EnvVar{
								{
									Name:  "VLAN_ID",
									Value: strconv.FormatInt(mgr.VlanID, 10),
								},
								{
									Name:  "NAMESPACE",
									Value: e.NamespaceName,
								},
								{
									Name:  "LOCK_NAME",
									Value: vlanmanv1.LeaderElectionLeaseName + "-" + mgr.OwnerNetworkName,
								},
								{
									Name:  "OWNER_NETWORK",
									Value: mgr.OwnerNetworkName,
								},
								{
									Name:  "LOCAL_GATEWAY_IP",
									Value: mgr.GatewayIP,
								},
								{
									Name:  "LOCAL_GATEWAY_SUBNET",
									Value: strconv.FormatInt(int64(mgr.GatewaySubnet), 10),
								},
								{
									Name:  "REMOTE_ROUTES",
									Value: strings.Join(mgr.RemoteRoutes, ","),
								},
								{
									Name:  "LOCAL_ROUTES",
									Value: strings.Join(mgr.LocalRoutes, ","),
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
	if len(mgr.ExcludedNodes) != 0 {
		spec.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/hostname",
									Operator: corev1.NodeSelectorOpNotIn,
									Values:   mgr.ExcludedNodes,
								},
							},
						},
					},
				},
			},
		}
	}
	return spec
}

func serviceForManagerSet(d ManagerSet, namespace string) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{d.OwnerNetworkName, vlanmanv1.ServiceNameSuffix}, "-"),
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				vlanmanv1.ManagerSetLabelKey: d.OwnerNetworkName,
			},
			InternalTrafficPolicy: u.Ptr(corev1.ServiceInternalTrafficPolicy("Local")),
			Ports: []corev1.ServicePort{
				{
					Name:       "manager",
					Protocol:   corev1.ProtocolTCP,
					Port:       61410,
					TargetPort: intstr.FromInt(61410),
				},
			},
		},
	}
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

	envs := d.Spec.Template.Spec.Containers[0].Env
	var vlanID int64 = -1
	gatewayIP := "empty"
	var gatewaySubnet int64 = -1
	remoteRoutes := []string{}
	localRoutes := []string{}
	for _, e := range envs {
		switch e.Name {
		case "VLAN_ID":
			vlanID, _ = strconv.ParseInt(e.Value, 10, 64)
		case "VLAN_SUBNET":
			gatewaySubnet, _ = strconv.ParseInt(e.Value, 10, 64)
		case "VLAN_IP":
			gatewayIP = e.Value
		case "REMOTE_ROUTES":
			remoteRoutes = strings.Split(e.Value, ",")
		case "LOCAL_ROUTES":
			localRoutes = strings.Split(e.Value, ",")
		default:
			continue
		}
	}

	return ManagerSet{
		OwnerNetworkName: d.Labels[vlanmanv1.ManagerSetLabelKey],
		VlanID:           vlanID,
		GatewayIP:        gatewayIP,
		RemoteRoutes:     remoteRoutes,
		LocalRoutes:      localRoutes,
		GatewaySubnet:    gatewaySubnet,
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
	gwIP, gwSn, found := strings.Cut(network.Spec.LocalGatewayIP, "/")
	if !found {
		gwSn = "32"
	}
	gwSnInt, _ := strconv.ParseInt(gwSn, 10, 64)
	return ManagerSet{
		OwnerNetworkName: network.Name,
		VlanID:           int64(network.Spec.VlanID),
		GatewayIP:        gwIP,
		RemoteRoutes:     network.Spec.RemoteSubnet,
		LocalRoutes:      network.Spec.LocalSubnet,
		GatewaySubnet:    gwSnInt,
		ExcludedNodes:    network.Spec.ExcludedNodes,
		Mappings:         network.Spec.Mappings,
	}
}
