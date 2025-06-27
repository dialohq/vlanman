package errors

import (
	"fmt"
	"runtime"
)

type CallerInfo struct {
	FunctionName string
	File         string
	Line         int
	Err          error
}

func (ci CallerInfo) String() string {
	if ci.Err != nil {
		return fmt.Sprintf("|Error extracting location information: %s|", ci.Err.Error())
	}
	return fmt.Sprintf("|%s in file %s at line %d|", ci.FunctionName, ci.File, ci.Line)
}

func GetCallerInfo() CallerInfo {
	pcs := make([]uintptr, 1)
	n := runtime.Callers(2, pcs)
	if n != 1 {
		return CallerInfo{
			Err: &UnrecoverableError{
				Context: "Couldn't get runtime.callers for logging, returned 0",
				Err:     ErrNilUnrecoverable,
			},
		}
	}
	framesPtr := runtime.CallersFrames(pcs)
	if framesPtr == nil {
		return CallerInfo{
			Err: &UnrecoverableError{
				Context: "Frames pointer returned from CallersFrames is nil",
				Err:     ErrNilUnrecoverable,
			},
		}
	}

	frame, _ := framesPtr.Next()
	return CallerInfo{
		FunctionName: frame.Function,
		Line:         frame.Line,
		File:         frame.File,
		Err:          nil,
	}
}
