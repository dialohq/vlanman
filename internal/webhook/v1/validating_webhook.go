package v1

import (
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
)

type ValidatingWebhookHandler struct {
	Client client.Client
	Config rest.Config
	Env    controller.Envs
}

// actionTypes
type creationAction struct{}
type deletionAction struct{}
type updateAction struct{}
type unknownAction struct{}

func writeResponseNoPatch(w http.ResponseWriter, in *admissionv1.AdmissionReview) {
	w.Header().Add("Content-Type", "application/json")
	r := noPatchResponse(in)
	rjson, err := json.Marshal(r)
	if err != nil {
		fmt.Println("Error marshalling response for no patch: ", err)
		return
	}
	w.Write(rjson)
}

func writeResponseDenied(w http.ResponseWriter, in *admissionv1.AdmissionReview, reason ...string) {
	w.Header().Add("Content-Type", "application/json")
	r := deniedResponse(in, reason...)
	rjson, err := json.Marshal(r)
	if err != nil {
		fmt.Println("Error marshalling response for denied: ", err)
		return
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
	in, err := u.ParseRequest(*r)
	if err != nil {
		err = errs.NewParsingError("Request", err)
		writeResponseDenied(w, in, err.Error())
	}
	action := getAction(in.Request)

	verdict := false
	var message error
	switch action.(type) {
	case creationAction:
		validator, err := NewCreationValidator(wh.Client, r.Context(), in.Request.Object.Raw)
		if err != nil {
			writeResponseDenied(w, in, fmt.Sprintf("Couldn't create a validator: %s", err.Error()))
			return
		}
		err = validator.Validate()
		if err != nil {
			message = err
		} else {
			verdict = true
		}

	case deletionAction:
		// message = &errs.UnimplementedError{Feature: "Deletion"}
		writeResponseNoPatch(w, in)
		return

	case updateAction:
		message = &errs.UnimplementedError{Feature: "Update"}

	default:
		message = &errs.InternalError{
			Context: fmt.Sprintf("Validating webhook couldn't determine action being taken: %s", *in),
		}
	}

	if verdict {
		writeResponseNoPatch(w, in)
		return
	}

	writeResponseDenied(w, in, message.Error())
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
