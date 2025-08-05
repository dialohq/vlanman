package errors

import (
	"errors"
	"fmt"
)

// Validating
var ErrTypeMismatch = errors.New("Unexpected type")

type TypeMismatchError struct {
	Where string
	Got   string
}

func (e *TypeMismatchError) Error() string {
	return fmt.Sprintf("Type mismatch error in %s, expected VlanNetwork but got: %s", e.Where, e.Got)
}

func (e *TypeMismatchError) Unwrap() error {
	return ErrTypeMismatch
}

func NewTypeMismatchError(where string, got any) error {
	return &TypeMismatchError{
		Where: where,
		Got:   fmt.Sprintf("%T", got),
	}
}

// Defaulting
var ErrMissingAnnotation = errors.New("At least one of the annotations are missing")

type MissingAnnotationError struct {
	Resource string
}

func (e *MissingAnnotationError) Error() string {
	return fmt.Sprintf("At least one of the required annotations is missing on resource: %s", e.Resource)
}

func (e *MissingAnnotationError) Unwrap() error {
	return ErrMissingAnnotation
}

var ErrNoIPInPool = errors.New("This pod's pool doesn't have a free IP address")

type NoIPInPoolError struct {
	Resource string
	Pool     string
}

func (e *NoIPInPoolError) Error() string {
	return fmt.Sprintf("Pod %s belongs to a pool (%s) with no free IP addresses", e.Resource, e.Pool)
}

func (e *NoIPInPoolError) Unwrap() error {
	return ErrNoIPInPool
}

var ErrNoManagerPods = errors.New("This network doesn't have any manager pods")

type NoManagerPodsError struct {
	Resource string
	Network  string
}

func (e *NoManagerPodsError) Error() string {
	return fmt.Sprintf("Pod %s belongs to a network (%s) with no existing manager pods (check logs of controller)", e.Resource, e.Network)
}

func (e *NoManagerPodsError) Unwrap() error {
	return ErrNoManagerPods
}

var ErrManagerNotReady = errors.New("Manager for this pod is not ready yet")

type ManagerNotReadyError struct {
	Resource string
	Manager  string
}

func (e *ManagerNotReadyError) Error() string {
	return fmt.Sprintf("This pod's (%s) manager (%s) is not ready yet, try again", e.Resource, e.Manager)
}

func (e *ManagerNotReadyError) Unwrap() error {
	return ErrManagerNotReady
}
