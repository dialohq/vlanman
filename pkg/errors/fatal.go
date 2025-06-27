package errors

import (
	"errors"
	"fmt"
)

var ErrFatalUnrecoverable = errors.New("Fatal unrecoverable error")

type FatalUnrecoverableError struct {
	Context string
	Err     error
}

func (e *FatalUnrecoverableError) Error() string {
	return fmt.Sprintf("Fatal unrecoverable error occured. Context: %s, error: %s", e.Context, e.Err.Error())
}

func (e *FatalUnrecoverableError) Unwrap() error {
	return e.Err
}
