package utils

import (
	"bytes"
	errs "dialo.ai/vlanman/pkg/errors"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"

	admissionv1 "k8s.io/api/admission/v1"
)

func Fatal(err error, l *slog.Logger, message string, args ...any) {
	if err != nil {
		l.Error(message, append([]any{"error", err}, args...)...)
		os.Exit(1)
	}
}

// https://ihateregex.io/expr/ip/
// IsValidIPv4 checks wheter a given IP address string is a valid IPv4 address
func IsValidIPv4(ip string) bool {
	regex := regexp.MustCompile(`(\b25[0-5]|\b2[0-4][0-9]|\b[01]?[0-9][0-9]?)(\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}`)
	return regex.MatchString(ip)
}

// https://github.com/slackhq/simple-kubernetes-webhook/blob/main/main.go
// ParseRequest parses the admission request into a go struct
func ParseRequest(r http.Request) (*admissionv1.AdmissionReview, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, fmt.Errorf("Content-Type: %q should be %q",
			r.Header.Get("Content-Type"), "application/json")
	}

	if r.Body == nil {
		return nil, &errs.FatalUnrecoverableError{
			Context: "While unmarshaling admission request in ParseRequest, request.Body is nil",
			Err:     errs.ErrFatalUnrecoverable,
		}
	}
	bodybuf := new(bytes.Buffer)
	bodybuf.ReadFrom(r.Body)
	body := bodybuf.Bytes()

	if len(body) == 0 {
		return nil, fmt.Errorf("admission request body is empty")
	}

	var a admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("could not parse admission review request: %v", err)
	}

	if a.Request == nil {
		return nil, fmt.Errorf("admission review can't be used: Request field is nil")
	}

	return &a, nil
}
