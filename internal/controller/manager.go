package controller

import (
	"encoding/json"
	"strconv"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	errs "dialo.ai/vlanman/pkg/errors"
	u "dialo.ai/vlanman/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type ManagerSet struct {
	OwnerNetworkName string
	VlanID           int64
	Gateways         []vlanmanv1.Gateway
	ManagerAffinity  *corev1.Affinity
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

func daemonSetFromManager(mgr ManagerSet, e Envs) (appsv1.DaemonSet, error) {
	poolsJSON, err := json.Marshal(mgr.Gateways)
	if err != nil {
		return appsv1.DaemonSet{}, &errs.ParsingError{
			Source: "ManagerSet pools",
			Err:    err,
		}
	}
	pools := string(poolsJSON)

	gatewaysJSON, err := json.Marshal(mgr.Gateways)
	if err != nil {
		return appsv1.DaemonSet{}, &errs.ParsingError{
			Source: "ManagerSet gateways",
			Err:    err,
		}
	}
	gateways := string(gatewaysJSON)

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
									Name:  "POOLS",
									Value: pools,
								},
								{
									Name:  "GATEWAYS",
									Value: gateways,
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
	if mgr.ManagerAffinity != nil {
		spec.Spec.Template.Spec.Affinity = mgr.ManagerAffinity
	}
	return spec, nil
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

func managerFromSet(d appsv1.DaemonSet) (ManagerSet, error) {
	managerAffinity := d.Spec.Template.Spec.Affinity

	envs := d.Spec.Template.Spec.Containers[0].Env
	var vlanID int64 = -1
	gateways := []vlanmanv1.Gateway{}
	for _, e := range envs {
		switch e.Name {
		case "VLAN_ID":
			vlanID, _ = strconv.ParseInt(e.Value, 10, 64)
		case "GATEWAYS":
			err := json.Unmarshal([]byte(e.Value), &gateways)
			if err != nil {
				return ManagerSet{}, err
			}
		default:
			continue
		}
	}

	return ManagerSet{
		OwnerNetworkName: d.Labels[vlanmanv1.ManagerSetLabelKey],
		VlanID:           vlanID,
		ManagerAffinity:  managerAffinity,
		Mappings:         []vlanmanv1.IPMapping{},
		Gateways:         gateways,
	}, nil
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
		Gateways:         network.Spec.Gateways,
		ManagerAffinity:  network.Spec.ManagerAffinity,
		Mappings:         network.Spec.Mappings,
	}
}
