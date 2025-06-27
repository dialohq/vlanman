package controller

import (
	"strconv"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type InterfacePod struct {
	ID   int
	PID  int
	Node string
}

func interfaceFromDaemon(p corev1.Pod, pid, id int, ttl *int32, image, networkName, pullPolicy string) batchv1.Job {
	var tgp int64 = 1
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{vlanmanv1.JobNamePrefix, networkName, p.Spec.NodeName}, "-"),
			Namespace: p.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &tgp,
					HostNetwork:                   true,
					HostPID:                       true,
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": p.Spec.NodeName,
					},
					Containers: []corev1.Container{
						{
							Name:            "create-vlan",
							Image:           image,
							ImagePullPolicy: corev1.PullPolicy(pullPolicy),
							Env: []corev1.EnvVar{
								{
									Name:  "PID",
									Value: strconv.FormatInt(int64(pid), 10),
								},
								{
									Name:  "ID",
									Value: strconv.FormatInt(int64(id), 10),
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
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}
