package errors

import (
	"errors"
	"fmt"
)

var ErrParsing = errors.New("Error parsing")

type ParsingError struct {
	Source string
	Err    error
}

func (e *ParsingError) Error() string {
	return fmt.Sprintf("Error parsing %s: %s", e.Source, e.Err.Error())
}

func (e *ParsingError) Unwrap() error {
	return e.Err
}

func NewParsingError(source string, err error) error {
	return fmt.Errorf("%w: %w", ErrParsing, &ParsingError{
		Source: source,
		Err:    err,
	})
}
