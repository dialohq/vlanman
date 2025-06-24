package errors

import (
	"errors"
	"fmt"
)

var ErrK8sClient = errors.New("K8s client request error")

type ClientRequestError struct {
	Location string
	Action   string
	Err      error
}

func (e *ClientRequestError) Error() string {
	return fmt.Sprintf("Error requesting %s in %s via k8s client: %s", e.Action, e.Location, e.Err.Error())
}

func (e *ClientRequestError) Unwrap() error {
	return e.Err
}

func NewClientRequestError(loc, act string, err error) error {
	return fmt.Errorf("%w: %w", ErrK8sClient, &ClientRequestError{
		Location: loc,
		Action:   act,
		Err:      err,
	})
}
