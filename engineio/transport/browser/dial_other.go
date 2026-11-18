//go:build !js

package browser

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

func (t *Transport) dial(u url.URL) (transport.Conn, error) {
	return nil, fmt.Errorf("browser: dial %s: only GOOS=js GOARCH=wasm has a browser: %w", u.Redacted(), errors.ErrUnsupported)
}
