package internal

import (
	"io"
	"net/http"
)

// ObserveHeaders installs the server's response observer before session startup.
// Package session installs the bridge, keeping engineio out of its imports.
var ObserveHeaders func(session io.Closer, f func(*http.Request, http.Header))
