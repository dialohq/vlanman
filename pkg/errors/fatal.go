package errors

import (
	"errors"
	"fmt"
)

var ErrUnrecoverable = errors.New("Fatal unrecoverable error")
var ErrNilUnrecoverable = errors.New("value required to progress is nil")

type UnrecoverableError struct {
	Context string
	Err     error
}

func (e *UnrecoverableError) Error() string {
	return fmt.Sprintf("Fatal unrecoverable error occured in %s. Context: %s, error: %s", GetCallerInfo(), e.Context, e.Err.Error())
}

func (e *UnrecoverableError) Unwrap() error {
	return e.Err
}
