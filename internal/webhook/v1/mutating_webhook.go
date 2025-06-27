package v1

import (
	"net/http"

	"dialo.ai/vlanman/internal/controller"

	u "dialo.ai/vlanman/pkg/utils"

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
// type dummyLocker struct{}

// func (dl *dummyLocker) Lock()   {}
// func (dl *dummyLocker) Unlock() {}

func (wh *MutatingWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	_ = log.FromContext(ctx, "webhook", true, "PodName", in.Request.Name)
	_ = false
	if in.Request.DryRun != nil && *in.Request.DryRun {
		_ = true
	}
}

// type jsonPatch struct {
// 	Op    string `json:"op"`
// 	Path  string `json:"path"`
// 	Value any    `json:"value"`
// }
