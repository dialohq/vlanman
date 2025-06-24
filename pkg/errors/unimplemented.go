package errors

import (
	"errors"
	"fmt"
)

var ErrUnimplemented = errors.New("Unimplemented")

type UnimplementedError struct {
	Feature string
}

func (e *UnimplementedError) Error() string {
	return fmt.Sprintf("unimplemented: %s", e.Feature)
}

func (e *UnimplementedError) Unwrap() error {
	return ErrUnimplemented
}
