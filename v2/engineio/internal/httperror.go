package internal

import (
	"net/http"
	"strconv"
)

// Engine.IO v4 error codes, the table of the protocol specification.
const (
	CodeUnknownTransport = 0
	CodeUnknownSID       = 1
	CodeBadHandshake     = 2
	CodeBadRequest       = 3
	CodeForbidden        = 4
	CodeUnsupportedProto = 5
)

var codeMessages = [...]string{
	CodeUnknownTransport: "Transport unknown",
	CodeUnknownSID:       "Session ID unknown",
	CodeBadHandshake:     "Bad handshake method",
	CodeBadRequest:       "Bad request",
	CodeForbidden:        "Forbidden",
	CodeUnsupportedProto: "Unsupported protocol version",
}

// WriteError answers with status and the Engine.IO v4 error body
// {"code":N,"message":"..."} for code, one of the Code constants.
func WriteError(w http.ResponseWriter, status, code int) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":` + strconv.Itoa(code) + `,"message":"` + codeMessages[code] + `"}`))
}
