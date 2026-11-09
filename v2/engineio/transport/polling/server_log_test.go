package polling

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
)

const rejected = "engineio: request rejected"

func pollRequest(method string) *http.Request {
	r := httptest.NewRequest(method, "/?transport=polling&EIO=4&sid=abc", nil)
	r.RemoteAddr = "192.0.2.1:4000"
	return r
}

// handshakeConn returns a connection created as Transport.Accept creates it: from the
// handshake request, which carries no sid.
func handshakeConn() *serverConn {
	return newServerConn(Default, httptest.NewRequest(http.MethodGet, "/?transport=polling&EIO=4", nil))
}

// TestServerBadMethod checks the invalid-method answer: HTTP 400 with the Engine.IO
// error body code 3, and one request-rejected record.
func TestServerBadMethod(t *testing.T) {
	rec := logtest.NewRecorder()
	logtest.SetDefault(t, rec)

	conn := handshakeConn()
	w := httptest.NewRecorder()
	conn.ServeHTTP(w, pollRequest(http.MethodPut))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"code":3,"message":"Bad request"}`, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	got := rec.Find(rejected)
	require.Len(t, got, 1)
	assert.Equal(t, "bad method", got[0]["reason"])
	assert.Equal(t, slog.LevelWarn.String(), got[0]["level"])
	assert.Equal(t, "polling", got[0]["transport"])
	assert.Equal(t, "192.0.2.1:4000", got[0]["remote_addr"])
	assert.Equal(t, "abc", got[0]["sid"])
}

// TestServerFlushLevels checks the level of the record that a failed poll leaves: WARN
// for the failure that ends the payload, DEBUG once the payload has failed with that
// error or the connection was closed.
func TestServerFlushLevels(t *testing.T) {
	poll := func(conn *serverConn) {
		w := httptest.NewRecorder()
		conn.ServeHTTP(w, pollRequest(http.MethodGet))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	}
	expired := func(conn *serverConn) {
		require.NoError(t, conn.SetWriteDeadline(time.Now().Add(-time.Second)))
	}
	levels := func(rec *logtest.Recorder) (out []string) {
		for _, m := range rec.Find(rejected) {
			assert.Equal(t, "flush", m["reason"])
			assert.Equal(t, "abc", m["sid"])
			assert.NotEmpty(t, m["err"])
			out = append(out, m["level"])
		}
		return out
	}
	warn, debug := slog.LevelWarn.String(), slog.LevelDebug.String()

	t.Run("failure then repeat", func(t *testing.T) {
		rec := logtest.NewRecorder()
		logtest.SetDefault(t, rec)
		conn := handshakeConn()
		expired(conn)
		poll(conn)
		poll(conn)
		assert.Equal(t, []string{warn, debug}, levels(rec))
	})

	t.Run("closed", func(t *testing.T) {
		rec := logtest.NewRecorder()
		logtest.SetDefault(t, rec)
		conn := handshakeConn()
		require.NoError(t, conn.Close())
		poll(conn)
		assert.Equal(t, []string{debug}, levels(rec))
	})

	t.Run("failed then closed", func(t *testing.T) {
		rec := logtest.NewRecorder()
		logtest.SetDefault(t, rec)
		conn := handshakeConn()
		expired(conn)
		poll(conn)
		require.NoError(t, conn.Close())
		poll(conn)
		assert.Equal(t, []string{warn, debug}, levels(rec))
	})
}
