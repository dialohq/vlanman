package errors

import (
	"errors"
	"fmt"
)

var ErrK8sClient = errors.New("K8s client request error")

type ClientRequestError struct {
	Action string
	Err    error
}

func (e *ClientRequestError) Error() string {
	return fmt.Sprintf("Error requesting %s in %s via k8s client: %s", e.Action, GetCallerInfo(), e.Err.Error())
}

func (e *ClientRequestError) Unwrap() error {
	return e.Err
}

func NewClientRequestError(act string, err error) error {
	return fmt.Errorf("%w: %w", ErrK8sClient, &ClientRequestError{
		Action: act,
		Err:    err,
	})
}

type RequestError struct {
	Action string
	Err    error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Error requesting %s in %s via http client: %s", e.Action, GetCallerInfo(), e.Err.Error())
}

func (e *RequestError) Unwrap() error {
	return e.Err
}
