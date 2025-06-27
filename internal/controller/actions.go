package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/pkg/comms"
	errs "dialo.ai/vlanman/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type DaemonSetTimeoutError struct {
	Name           string
	Ready          int
	Total          int
	CleanupSuccess bool
	CleanupErr     error
}

var ErrDaemonPodTimeout = errors.New("Daemon pod timeout error")

func (e *DaemonSetTimeoutError) Error() string {
	var cleanupMsg string
	if e.CleanupSuccess {
		cleanupMsg = "Cleaned up successfully."
	} else {
		cleanupMsg = fmt.Sprintf("Cleanup failed: %s", e.CleanupErr.Error())
	}
	return fmt.Sprintf("Timeout waiting for '%s' daemonset to become available after %d tries (%d Ready, %d Total). %s",
		e.Name,
		vlanmanv1.WaitForDaemonTimeout,
		e.Ready,
		e.Total,
		cleanupMsg,
	)
}

func (e *DaemonSetTimeoutError) Unwrap() error {
	return ErrDaemonSetTimeout
}

type DaemonPodTimeoutError struct {
	Name           string
	CleanupSuccess bool
	CleanupErr     error
}

var ErrDaemonSetTimeout = errors.New("Daemonset timeout error")

func (e *DaemonPodTimeoutError) Error() string {
	var cleanupMsg string
	if e.CleanupSuccess {
		cleanupMsg = "Cleaned up successfully."
	} else {
		cleanupMsg = fmt.Sprintf("Cleanup failed: %s", e.CleanupErr.Error())
	}
	return fmt.Sprintf("Timeout waiting for daemon '%s' to get assigned IP after %d tries. %s",
		e.Name,
		vlanmanv1.WaitForDaemonTimeout,
		cleanupMsg,
	)
}

func (e *DaemonPodTimeoutError) Unwrap() error {
	return ErrDaemonPodTimeout
}

type Action interface {
	Do(context.Context, *VlanmanReconciler) error
}

type CreateManagerAction struct {
	Manager ManagerSet
}

func (a *CreateManagerAction) Do(ctx context.Context, r *VlanmanReconciler) error {
	log := log.FromContext(ctx)
	daemonSet := daemonSetFromManager(a.Manager, r.Env)
	err := r.Client.Create(ctx, &daemonSet)
	if err != nil {
		return &errs.ClientRequestError{
			Action: "Create daemonset",
			Err:    err,
		}
	}
	err = r.Client.Get(ctx, types.NamespacedName{Name: daemonSet.Name, Namespace: daemonSet.Namespace}, &daemonSet)
	timeout := 1
	for ((err != nil && apierrors.IsNotFound(err)) ||
		reflect.DeepEqual(daemonSet.Status, appsv1.DaemonSetStatus{})) &&
		timeout <= vlanmanv1.WaitForDaemonTimeout {

		log.Info("Waiting for daemonset")
		time.Sleep(time.Second / 2)

		err = r.Client.Get(ctx, types.NamespacedName{Name: daemonSet.Name, Namespace: daemonSet.Namespace}, &daemonSet)
		timeout += 1
	}

	if err != nil {
		return errs.NewClientRequestError("CreateManagerAction", "GetDaemonset", err)
	}

	// not created, don't have to clean up
	if timeout > vlanmanv1.WaitForDaemonTimeout {
		return &DaemonSetTimeoutError{
			Name:           a.Manager.OwnerNetworkName,
			Ready:          0,
			Total:          0,
			CleanupSuccess: true,
			CleanupErr:     nil,
		}
	}

	// use the same timeout counter as above, same thing really same limit should apply
	// TODO: maybe wait for daemon to not be not found before this loop?
	for timeout <= vlanmanv1.WaitForDaemonTimeout && daemonSet.Status.NumberUnavailable != 0 {
		stats := fmt.Sprintf("%d/%d/%d",
			daemonSet.Status.CurrentNumberScheduled,
			daemonSet.Status.DesiredNumberScheduled,
			daemonSet.Status.NumberUnavailable,
		)
		tries := fmt.Sprintf("%d/%d",
			timeout,
			vlanmanv1.WaitForDaemonTimeout,
		)
		err = r.Client.Get(ctx, types.NamespacedName{Name: daemonSet.Name, Namespace: r.Env.NamespaceName}, &daemonSet)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return &errs.ClientRequestError{
					Action: "Get Daemonset",
					Err:    err,
				}
			}
		}
		log.Info("Waiting for daemons 100% availible", "amount", stats, "tries", tries)
		timeout += 1
		time.Sleep(time.Second / 2)
	}
	if timeout > vlanmanv1.WaitForDaemonTimeout {
		err := r.Client.Delete(ctx, &daemonSet)
		cleanupSuccess := true
		var cleanupError error = nil
		if err != nil {
			cleanupError = err
			cleanupSuccess = false
		}

		return &DaemonSetTimeoutError{
			Name:           a.Manager.OwnerNetworkName,
			Ready:          int(daemonSet.Status.CurrentNumberScheduled),
			Total:          int(daemonSet.Status.DesiredNumberScheduled),
			CleanupSuccess: cleanupSuccess,
			CleanupErr:     cleanupError,
		}
	}
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement(vlanmanv1.ManagerSetLabelKey, "==", []string{a.Manager.OwnerNetworkName})
	if err != nil {
		return &errs.InternalError{Context: fmt.Sprintf("Error creating a label selector requirement in createDesiredState: %s", err.Error())}
	}
	selector = selector.Add(*requirement)
	opts := client.ListOptions{
		LabelSelector: selector,
	}
	pods := &corev1.PodList{Items: []corev1.Pod{}}
	for len(pods.Items) != int(daemonSet.Status.DesiredNumberScheduled) {
		ready := fmt.Sprintf("%d/%d", len(pods.Items), int(daemonSet.Status.DesiredNumberScheduled))
		log.Info("Waiting for daemonset to create all pods", "ready", ready)
		err = r.Client.List(ctx, pods, &opts)
		if err != nil {
			return errs.NewClientRequestError("CreateManager", "ListPods", err)
		}
	}
	log.Info("Pods", "number", len(pods.Items), "Desired", daemonSet.Status.DesiredNumberScheduled)

	for _, pod := range pods.Items {
		tries := 1
		for pod.Status.PodIP == "" && tries <= vlanmanv1.WaitForDaemonTimeout {
			triesString := fmt.Sprintf("%d/%d", tries, vlanmanv1.WaitForDaemonTimeout)
			log.Info("Waiting for daemon before starting job", "pod", pod.Name, "tries", triesString)
			err := r.Client.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &pod)
			if err != nil {
				return &errs.ClientRequestError{
					Action: "Wait for pod",
					Err:    err,
				}
			}
			tries += 1
		}
		if tries > vlanmanv1.WaitForDaemonTimeout {
			return &DaemonPodTimeoutError{
				Name:           pod.Name,
				CleanupSuccess: false,
				CleanupErr:     &errs.UnimplementedError{Feature: "Cleanup unimplemented"}, // TODO
			}
		}

		resp, err := http.Get(fmt.Sprintf("http://%s:61410/pid", pod.Status.PodIP))
		if err != nil {
			return &errs.RequestError{
				Action: "Get PID",
				Err:    err,
			}
		}

		if resp == nil || resp.Body == nil {
			return &errs.RequestError{
				Action: "Get PID",
				Err:    fmt.Errorf("response or response body is nil"),
			}
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return &errs.ParsingError{
				Source: "CreatemanagerAction/CreateJob/ReadBodyPIDResponse",
				Err:    err,
			}
		}

		PID := &comms.PIDResponse{}
		err = json.Unmarshal(body, PID)
		if err != nil {
			return &errs.ParsingError{
				Source: "CreatemanagerAction/CreateJob/UnmarshalBodyPIDResponse",
				Err:    err,
			}
		}

		job := interfaceFromDaemon(pod, PID.PID, int(a.Manager.VlanID), r.Env.TTL, r.Env.InterfacePodImage, a.Manager.OwnerNetworkName, r.Env.InterfacePodPullPolicy)
		err = r.Client.Create(ctx, &job)
		if err != nil {
			// TODO: there should be cleanup here as well
			return &errs.ClientRequestError{
				Action: "Create job",
				Err:    err,
			}
		}
		resp, err = http.Get(fmt.Sprintf("http://%s:61410", pod.Status.PodIP))
		if err != nil {
			return &errs.RequestError{
				Action: "CheckDaemonReady",
				Err:    err,
			}
		}
		if resp == nil {
			return &errs.RequestError{
				Action: "CheckDaemonReady",
				Err:    fmt.Errorf("Response is nil"),
			}
		}
		for resp.StatusCode != 200 && timeout <= vlanmanv1.WaitForDaemonTimeout {
			triesString := fmt.Sprintf("%d/%d", tries, vlanmanv1.WaitForDaemonTimeout)
			log.Info("Waiting for pod to return ready (200)", "received", resp.StatusCode, "tries", triesString)
			resp, err = http.Get(fmt.Sprintf("http://%s:61410/ready", pod.Status.PodIP))
			if err != nil {
				return &errs.RequestError{
					Action: "CheckDaemonReady",
					Err:    err,
				}
			}
			time.Sleep(time.Second / 2)
			tries += 1
		}
	}
	return nil
}

type DeleteManagerAction struct {
	Manager ManagerSet
}

func (a *DeleteManagerAction) Do(ctx context.Context, r *VlanmanReconciler) error {
	fmt.Println("Deleting manager: ", a.Manager.OwnerNetworkName)
	daemonSet := daemonSetFromManager(a.Manager, r.Env)
	err := r.Client.Delete(ctx, &daemonSet)
	if err != nil {
		return &errs.ClientRequestError{
			Action: "Delete daemonset",
			Err:    err,
		}
	}
	return nil
}

type ThrowErrorAction struct {
	Err error
}

func (a *ThrowErrorAction) Do(ctx context.Context, r *VlanmanReconciler) error {
	return a.Err
}
