package v1

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"

	u "dialo.ai/vlanman/pkg/utils"
	"github.com/jrhouston/k8slock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

// For dry run, we don't want side effects
// so creating a lease is off the table
type dummyLocker struct{}

func (dl *dummyLocker) Lock()   {}
func (dl *dummyLocker) Unlock() {}

func (wh *MutatingWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	in, err := u.ParseRequest(*r)
	if err != nil {
		logger := log.FromContext(ctx, "webhook", true)
		logger.Error(err, "Error parsing request", "request", *r)
		writeResponseDenied(w, in, "error parsing request")
		return
	}
	if in.Request.Kind.Kind != "Pod" {
		writeResponseNoPatch(w, in)
		return
	}
	logger := log.FromContext(ctx, "webhook", true, "PodName", in.Request.Name)
	isDryRun := false
	if in.Request.DryRun != nil && *in.Request.DryRun == true {
		isDryRun = true
	}

	var pod corev1.Pod
	json.Unmarshal(in.Request.Object.Raw, &pod)
	networkName, ok := pod.Annotations[vlanmanv1.PodVlanmanNetworkAnnotation]
	if !ok {
		writeResponseNoPatch(w, in)
		return
	}

	clientSet, err := kubernetes.NewForConfig(&wh.Config)
	if err != nil {
		logger.Error(err, "Couldn't create clientset for managing leases")
		writeResponseDenied(w, in, "couldn't create clientset")
		return
	}
	var locker sync.Locker
	if isDryRun || wh.Env.IsTest {
		locker = &dummyLocker{}
	} else {
		// Sometimes in between this checks for existence of
		// lease (in NewLocker constructor) and it's creation
		// of lease another instance already created it and
		// it errors with "Already exists".
		// This is a rough work around.
		leaseName := strings.Join([]string{vlanmanv1.LeasePrefix, networkName, vlanmanv1.LeasePostfix}, "-")
		for {
			locker, err = k8slock.NewLocker(leaseName,
				k8slock.TTL(5*time.Second),
				k8slock.Namespace(wh.Env.NamespaceName),
				k8slock.Clientset(clientSet),
			)
			if err == nil {
				break
			}

			if errors.IsAlreadyExists(err) {
				n := ((rand.Int() % 10) + 1) * 100
				logger.Info("Lease already exists, sleeping randomly", "time", n)
				time.Sleep(time.Duration(n) * time.Millisecond)
			} else {
				logger.Error(err, "Couldn't create a lease locker instance")
				writeResponseDenied(w, in, "Couldnt create lease")
				return
			}
		}
	}

	// prevent race conditions for IP address
	// from vlan networks status with leases
	locker.Lock()
	vlanNetwork := &vlanmanv1.VlanNetwork{}
	nsn := types.NamespacedName{
		Namespace: "",
		Name:      networkName,
	}
	err = wh.Client.Get(ctx, nsn, vlanNetwork)
	if err != nil {
		writeResponseDenied(w, in, "Couldn't fetch vlan network")
		return
	}

	writeResponseNoPatch(w, in)
	return
}

type jsonPatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}
