package polling

import (
	"errors"
	"mime"
	"strings"
)

// Addr is the net.Addr of a polling connection, which spans many HTTP
// requests rather than one network connection. On the server, LocalAddr is
// the Host header and RemoteAddr the RemoteAddr of the request that opened the
// connection; on the client, LocalAddr is empty and RemoteAddr is the host of
// the dialed URL.
type Addr struct {
	// Host is the address in host or host:port form, or empty when unknown.
	Host string
}

// Network returns "tcp".
func (a Addr) Network() string {
	return "tcp"
}

// String returns Host.
func (a Addr) String() string {
	return a.Host
}

func mimeIsSupportBinary(m string) (bool, error) {
	typ, params, err := mime.ParseMediaType(m)
	if err != nil {
		return false, err
	}

	switch typ {
	case "application/octet-stream":
		return true, nil

	case "text/plain":
		charset := strings.ToLower(params["charset"])
		if charset != "utf-8" {
			return false, errors.New("invalid charset")
		}
		return false, nil
	}

	return false, errors.New("invalid content-type")
}

// requestBodyIsBinary reports how a polling POST body is decoded, from the value
// of its Content-Type header. As in Engine.IO 3.6.2 only application/octet-stream
// selects the binary payload; every other value, a missing header and a value that
// is not a valid media type included, selects the text payload. The type is
// compared case-insensitively and its parameters are ignored, so a malformed
// parameter list does not turn a binary body into a text one.
func requestBodyIsBinary(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "application/octet-stream")
}
