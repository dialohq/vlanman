package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	errs "dialo.ai/vlanman/pkg/errors"

	admissionv1 "k8s.io/api/admission/v1"
)

func Ptr[T any](a T) *T {
	return &a
}

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
		return nil, &errs.UnrecoverableError{
			Context: "While unmarshaling admission request in ParseRequest, request.Body is nil",
			Err:     errs.ErrUnrecoverable,
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

type CallerInfo struct {
	FunctionName string
	File         string
	Line         int
	Err          error
}

func (ci CallerInfo) String() string {
	if ci.Err != nil {
		return fmt.Sprintf("Error extracting location information: %s", ci.Err.Error())
	}
	return fmt.Sprintf("%s in file %s at line %d", ci.FunctionName, ci.File, ci.Line)
}

func GetCallerInfo() CallerInfo {
	pcs := make([]uintptr, 1)
	n := runtime.Callers(2, pcs)
	if n != 1 {
		return CallerInfo{
			Err: &errs.UnrecoverableError{
				Context: "Couldn't get runtime.callers for logging, returned 0",
				Err:     errs.ErrNilUnrecoverable,
			},
		}
	}
	framesPtr := runtime.CallersFrames(pcs)
	if framesPtr == nil {
		return CallerInfo{
			Err: &errs.UnrecoverableError{
				Context: "Frames pointer returned from CallersFrames is nil",
				Err:     errs.ErrNilUnrecoverable,
			},
		}
	}

	frame, _ := framesPtr.Next()
	return CallerInfo{
		FunctionName: frame.Function,
		Line:         frame.Line,
		File:         frame.File,
		Err:          nil,
	}
}

func PopulateStatus(status vlanmanv1.VlanNetworkStatus, poolNames ...string) vlanmanv1.VlanNetworkStatus {
	if status.PendingIPs == nil {
		status.PendingIPs = map[string]map[string]string{}
	}
	if status.FreeIPs == nil {
		status.FreeIPs = map[string][]string{}
	}
	for _, poolName := range poolNames {
		if _, ok := status.FreeIPs[poolName]; !ok {
			status.FreeIPs[poolName] = []string{}
		}
	}
	return status
}

func IpToByteArray(ip string) ([]byte, error) {
	octets := strings.Split(ip, ".")
	octet_bytes := []byte{}
	for _, o := range octets {
		i, err := strconv.ParseUint(o, 10, 8)
		if err != nil {
			return nil, err
		}
		octet_bytes = append(octet_bytes, byte(i))
	}
	return octet_bytes, nil
}
