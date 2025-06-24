package errors

import (
	"errors"
	"fmt"
)

var ErrInternal = errors.New("Internal error")

type InternalError struct {
	Context string
}

func (e *InternalError) Error() string {
	return fmt.Sprintf("Internal error, please open an issue on github with this error message and related information. Context: %s", e.Context)
}

func (e *InternalError) Unwrap() error {
	return ErrInternal
}
