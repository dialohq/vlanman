package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"dialo.ai/vlanman/internal/controller"
	errs "dialo.ai/vlanman/pkg/errors"
	u "dialo.ai/vlanman/pkg/utils"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ValidatingWebhookHandler struct {
	Client client.Client
	Config rest.Config
	Env    controller.Envs
}

// actionTypes
type (
	creationAction struct{}
	deletionAction struct{}
	updateAction   struct{}
	unknownAction  struct{}
)

func writeResponseNoPatch(ctx context.Context, w http.ResponseWriter, in *admissionv1.AdmissionReview) {
	logger := log.FromContext(ctx)
	w.Header().Add("Content-Type", "application/json")
	r := noPatchResponse(in)
	rjson, err := json.Marshal(r)
	if err != nil {
		err = errs.NewParsingError("Marshalling no patch response in validating webhook", err)
		logger.Error(err, "Error writing no patch response in validating webhook")
	}
	w.Write(rjson)
}

func writeResponseDenied(ctx context.Context, w http.ResponseWriter, in *admissionv1.AdmissionReview, reason ...string) {
	logger := log.FromContext(ctx)
	w.Header().Add("Content-Type", "application/json")
	r := deniedResponse(in, reason...)
	rjson, err := json.Marshal(r)
	if err != nil {
		err = errs.NewParsingError("Marshalling denied response in validating webhook", err)
		logger.Error(err, "Error writing denied response in validating webhook")
	}
	w.Write(rjson)
}

func getAction(req *admissionv1.AdmissionRequest) any {
	if len(req.OldObject.Raw) == 0 && len(req.Object.Raw) != 0 {
		return creationAction{}
	}

	if len(req.OldObject.Raw) != 0 && len(req.Object.Raw) == 0 {
		return deletionAction{}
	}

	if len(req.OldObject.Raw) != 0 && len(req.Object.Raw) != 0 {
		return updateAction{}
	}

	return unknownAction{}
}

func (wh *ValidatingWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		fmt.Println("Validating webhook error", &errs.UnrecoverableError{
			Context: "In ValidatingWebhookHandler the request is nil",
			Err:     errs.ErrNilUnrecoverable,
		})
		return
	}
	ctx := r.Context()
	log := log.FromContext(ctx)

	in, err := u.ParseRequest(*r)
	if err != nil || in == nil {
		log.Error(&errs.UnrecoverableError{
			Context: "Parsing request in ValidatingWebhookHandler",
			Err: &errs.ParsingError{
				Source: "Request",
				Err:    err,
			},
		}, "Validating webhook error")
		return
	}
	action := getAction(in.Request)
	allow := false
	var message error = nil
	switch action.(type) {
	case creationAction:
		validator, err := NewCreationValidator(wh.Client, r.Context(), in.Request.Object.Raw)
		if err != nil {
			writeResponseDenied(ctx, w, in, fmt.Sprintf("Couldn't create a validator: %s", err.Error()))
			return
		}
		err = validator.Validate()
		if err != nil {
			message = err
		} else {
			allow = true
		}

	case deletionAction:
		validator, err := NewDeletionValidator(wh.Client, ctx, in.Request.OldObject.Raw)
		if err != nil {
			writeResponseDenied(ctx, w, in, fmt.Sprintf("Couldn't create a validator: %s", err.Error()))
			return
		}
		err = validator.Validate()
		if err != nil {
			message = err
		} else {
			allow = true
		}

	case updateAction:
		validator, err := NewUpdateValidator(wh.Client, r.Context(), in.Request.Object.Raw, in.Request.OldObject.Raw)
		if err != nil {
			writeResponseDenied(ctx, w, in, fmt.Sprintf("Couldn't create a validator: %s", err.Error()))
			return
		}
		err = validator.Validate()
		if err != nil {
			message = err
		} else {
			allow = true
		}

	default:
		message = &errs.InternalError{
			Context: fmt.Sprintf("Validating webhook couldn't determine action being taken: %s", *in),
		}
	}

	if allow {
		writeResponseNoPatch(ctx, w, in)
		return
	}

	if message != nil {
		writeResponseDenied(ctx, w, in, message.Error())
	} else {
		writeResponseDenied(ctx, w, in, "empty message")
	}
}

func noPatchResponse(in *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     in.Request.UID,
			Allowed: true,
		},
	}
}

func deniedResponse(in *admissionv1.AdmissionReview, reasonList ...string) *admissionv1.AdmissionReview {
	reason := "No reason provided."
	if len(reasonList) != 0 {
		reason = reasonList[0]
	}
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     in.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				TypeMeta: metav1.TypeMeta{},
				Message:  reason,
			},
		},
	}
}
