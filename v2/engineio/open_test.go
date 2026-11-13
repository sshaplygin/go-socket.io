package engineio_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/client"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/payload"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// openParams performs a handshake on tr and returns the parameters of the OPEN packet.
func openParams(t *testing.T, srv *engineio.Server, tr transport.Transport) transport.ConnParameters {
	t.Helper()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Close() }) // ends the open sessions, so ts.Close does not wait for a poll
	u, err := url.Parse(ts.URL + "/?EIO=4")
	require.NoError(t, err)
	cl, err := tr.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	if o, ok := cl.(client.Opener); ok {
		params, err := o.Open()
		require.NoError(t, err)
		return params
	}
	_, pt, r, err := cl.NextReader()
	require.NoError(t, err)
	require.Equal(t, packet.OPEN, pt)
	params, err := transport.ReadConnParameters(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return params
}

// TestOpenAdvertisesMaxPayload: the OPEN packet carries maxPayload, the POST limit the
// polling transport enforces (payload.DefaultMaxPayload unless polling.Transport
// sets MaxPayload). A server without polling advertises the message limit of its
// websocket transport, as Node's maxHttpBufferSize bounds both.
func TestOpenAdvertisesMaxPayload(t *testing.T) {
	newServer := func(trs ...transport.Transport) *engineio.Server {
		srv := engineio.NewServer(&engineio.Options{Transports: trs})
		t.Cleanup(func() { _ = srv.Close() })
		return srv
	}

	t.Run("default", func(t *testing.T) {
		srv := newServer()
		assert.Equal(t, payload.DefaultMaxPayload, openParams(t, srv, polling.Default).MaxPayload)
		assert.Equal(t, payload.DefaultMaxPayload, openParams(t, srv, websocket.Default).MaxPayload)
	})

	t.Run("polling limit", func(t *testing.T) {
		const limit = 1234
		srv := newServer(&polling.Transport{MaxPayload: limit}, &websocket.Transport{MaxPayload: 999})
		params := openParams(t, srv, polling.Default)
		assert.Equal(t, limit, params.MaxPayload)
		assert.Equal(t, limit, openParams(t, srv, websocket.Default).MaxPayload)

		// The advertised value is the one the transport enforces.
		post := func(size int) int {
			ts := httptest.NewServer(srv)
			defer ts.Close()
			body := "4" + strings.Repeat("a", size-1)
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/?EIO=4&transport=polling&sid="+params.SID, strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			return resp.StatusCode
		}
		assert.Equal(t, http.StatusRequestEntityTooLarge, post(limit+1))
	})

	t.Run("polling after websocket", func(t *testing.T) {
		srv := newServer(&websocket.Transport{MaxPayload: 2222}, &polling.Transport{MaxPayload: 3333})
		assert.Equal(t, 3333, openParams(t, srv, websocket.Default).MaxPayload,
			"the polling limit is advertised whatever the transport order")
	})

	t.Run("websocket only", func(t *testing.T) {
		srv := newServer(&websocket.Transport{MaxPayload: 4321})
		assert.Equal(t, 4321, openParams(t, srv, websocket.Default).MaxPayload)
	})
}
