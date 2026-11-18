//go:build js && wasm

package browser

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"syscall/js"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/logger"
)

func (t *Transport) dial(u url.URL) (transport.Conn, error) {
	constructor := js.Global().Get("WebSocket")
	if constructor.Type() != js.TypeFunction {
		return nil, errors.New("browser: the WebSocket API is not available")
	}
	args := []any{u.String()}
	if len(t.Subprotocols) != 0 {
		protocols := make([]any, len(t.Subprotocols))
		for i, p := range t.Subprotocols {
			protocols[i] = p
		}
		args = append(args, protocols)
	}
	c := newConn(u)
	err := catch(func() {
		c.ws = constructor.New(args...)
		c.ws.Set("binaryType", "arraybuffer")
	})
	if err != nil {
		return nil, fmt.Errorf("browser: dial %s: %w", u.Redacted(), err)
	}
	c.attach()

	var expired <-chan time.Time
	if t.HandshakeTimeout > 0 {
		timer := time.NewTimer(t.HandshakeTimeout)
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-c.opened:
		return c, nil
	case <-c.done:
		// The close event released the handlers. An open event that came first wins.
		select {
		case <-c.opened:
			return c, nil
		default:
		}
		return nil, fmt.Errorf("browser: dial %s: the connection failed: %w", u.Redacted(), c.closeErr())
	case <-expired:
		if err := c.call("close"); err != nil {
			logger.Log.Debug("browser: close after handshake timeout failed", "err", err)
		}
		c.release()
		return nil, fmt.Errorf("browser: dial %s: handshake timeout: %w", u.Redacted(), os.ErrDeadlineExceeded)
	}
}
