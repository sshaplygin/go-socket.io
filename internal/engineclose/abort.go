// Package engineclose connects forced root shutdowns to the engine's private
// abort path without adding methods to the public connection interfaces.
package engineclose

import "io"

// AbortSession is installed by engineio/session during package initialization.
// It returns handled=false for connections outside that package.
var AbortSession func(io.Closer) (handled bool, err error)

// Abort interrupts even an orderly close already waiting for a polling request.
// Other engine implementations retain their ordinary Close behavior.
func Abort(c io.Closer) error {
	if AbortSession != nil {
		if handled, err := AbortSession(c); handled {
			return err
		}
	}
	return c.Close()
}
