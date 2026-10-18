package engineio

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
)

func TestEngineHeadersSurviveSessionHookReplacement(t *testing.T) {
	for _, name := range []string{"clear", "replace"} {
		t.Run(name, func(t *testing.T) {
			records := &hookRecords{}
			f := newParityWire(t, []Option{WithConnInitor(func(_ *http.Request, c Conn) {
				var hooks *sessionImpl.Hooks
				if name == "replace" {
					hooks = &sessionImpl.Hooks{}
				}
				c.(*sessionImpl.Session).SetHooks(hooks)
			}), WithHooks(&Hooks{
				InitialHeaders: func(_ *http.Request, _ http.Header) { records.add("initial") },
				Headers:        func(r *http.Request, _ http.Header) { records.add(r.Method) },
			})})
			require.Equal(t, []string{"initial", "GET"}, records.all())
			reads := parityReader(f.conn)
			status, _ := f.request(t, http.MethodPost, f.poll, "2:4x")
			require.Equal(t, http.StatusOK, status)
			parityMessage(t, reads, "x")
			written := parityWrite(f.conn, "y")
			status, body := f.request(t, http.MethodGet, f.poll, "")
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, "2:4y", body)
			require.NoError(t, hookReceive(t, written))
			require.Equal(t, []string{"initial", "GET", "POST", "GET"}, records.all())
		})
	}
}
