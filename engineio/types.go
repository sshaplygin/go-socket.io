package engineio

import (
	"net/http"
)

// CheckerFunc checks a new handshake. Its headers are included in the response;
// an error rejects the handshake with HTTP 403 and the fixed message Forbidden.
type CheckerFunc func(*http.Request) (http.Header, error)

// ConnInitorFunc initializes a registered session on the handshake request's
// goroutine, with the request live, before OPEN is written and Accept can return
// it. Closing the session here prevents it from reaching Accept.
type ConnInitorFunc func(*http.Request, Conn)
