package polling

import (
	"errors"
	"fmt"
)

// Error is payload error.
type Error interface {
	Error() string
	Temporary() bool
}

// OpError is operation error.
type OpError struct {
	Op  string
	Err error
}

func newOpError(op string, err error) error {
	return &OpError{
		Op:  op,
		Err: err,
	}
}

func (e *OpError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, e.Err.Error())
}

// Timeout reports whether Err is an expired read or write deadline.
func (e *OpError) Timeout() bool {
	if oe, ok := e.Err.(interface{ Timeout() bool }); ok {
		return oe.Timeout()
	}
	return false
}

// Temporary returns true if error can retry.
func (e *OpError) Temporary() bool {
	if oe, ok := e.Err.(Error); ok {
		return oe.Temporary()
	}
	return false
}

type retryError struct {
	err string
}

func (e retryError) Error() string {
	return e.err
}

func (e retryError) Temporary() bool {
	return true
}

var errPaused = retryError{"paused"}

// timeoutError is an expired payload deadline. Unlike retryError it is not temporary.
type timeoutError struct{}

func (timeoutError) Error() string { return "timeout" }
func (timeoutError) Timeout() bool { return true }

var errTimeout error = timeoutError{}

var errOverlap = errors.New("overlap")

var errDetached = errors.New("writer detached")
