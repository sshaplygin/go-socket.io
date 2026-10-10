// Package internal holds what the engineio packages share without exporting it.
package internal

import "io"

// Shutdown closes session, a *session.Session, with the close reason "server shutting
// down". Package session sets it; engineio.Server.Close calls it.
var Shutdown func(session io.Closer) error
