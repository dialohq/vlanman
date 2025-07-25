package errors

import (
	"errors"
	"fmt"
)

var ErrTryAgain = errors.New("Recoverable error")

type TryAgainError struct {
	Err error
}

func (e *TryAgainError) Error() string {
	return fmt.Sprintf("%s: %s", ErrTryAgain.Error(), e.Err.Error())
}

func (e *TryAgainError) Unwrap() error {
	return ErrTryAgain
}
