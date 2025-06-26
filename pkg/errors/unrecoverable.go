package errors

import (
	"errors"
	"strings"
)

var ErrUnrecoverable = errors.New("Encountered unrecoverable errors")

type UnrecoverableErrorsEncountered struct {
	Errors []error
}

func (e *UnrecoverableErrorsEncountered) Error() string {
	msg := "Unrecoverable errors encountered:"
	errStr := []string{}
	for _, e := range e.Errors {
		errStr = append(errStr, e.Error())
	}
	msg += strings.Join(errStr, " |+| ")
	return msg
}

func (e UnrecoverableErrorsEncountered) Unwrap() error {
	return ErrUnrecoverable
}
