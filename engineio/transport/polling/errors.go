package polling

import (
	"errors"
	"fmt"
)

// Error is a payload error that reports whether the failed operation can be
// retried. OpError.Temporary consults it on its Err.
type Error interface {
	// Error returns the error text.
	Error() string
	// Temporary reports whether the operation can succeed when retried, as
	// for a payload paused during a transport upgrade.
	Temporary() bool
}

// OpError is the error a polling connection returns for a failed operation.
// OpError has no Unwrap method; use Timeout and Temporary, or inspect Err.
type OpError struct {
	// Op names the operation: "read" or "write" of the payload, "payload" for
	// a paused payload, and on the client "get" or "post" for a failed poll or
	// send request.
	Op string
	// Err is the cause.
	Err error
}

func newOpError(op string, err error) error {
	return &OpError{
		Op:  op,
		Err: err,
	}
}

// Error returns Op + ": " + the text of Err.
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
