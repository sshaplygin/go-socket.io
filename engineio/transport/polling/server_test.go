package polling

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// TestServerIgnoresJSONPParameter checks that `j` no longer selects JSONP (ROADMAP
// Stage V1, D4): the response is the ordinary payload, not a script.
func TestServerIgnoresJSONPParameter(t *testing.T) {
	must := require.New(t)

	var scValue atomic.Value

	pollingTransport := Default
	conn := make(chan transport.Conn, 1)

	handler := func(w http.ResponseWriter, r *http.Request) {
		c := scValue.Load()
		if c == nil {
			co, err := pollingTransport.Accept(w, r)
			require.NoError(t, err)

			scValue.Store(co)
			c = co
			conn <- co
		}
		c.(http.Handler).ServeHTTP(w, r)
	}

	httpSvr := httptest.NewServer(http.HandlerFunc(handler))
	defer httpSvr.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sc := <-conn

		defer func() {
			must.NoError(sc.Close())
		}()

		w, err := sc.NextWriter(eioparser.Binary, eioparser.MESSAGE)
		require.NoError(t, err)

		_, err = w.Write([]byte("hello"))
		require.NoError(t, err)

		err = w.Close()
		require.NoError(t, err)

		w, err = sc.NextWriter(eioparser.String, eioparser.MESSAGE)
		require.NoError(t, err)

		_, err = w.Write([]byte("world"))
		require.NoError(t, err)

		err = w.Close()
		require.NoError(t, err)
	}()

	{
		u := httpSvr.URL + "?b64=1&j=jsonp_f1"
		resp, err := http.Get(u)
		require.NoError(t, err)

		defer func() {
			err = resp.Body.Close()
			require.NoError(t, err)
		}()

		assert.Equal(t, "text/plain; charset=UTF-8", resp.Header.Get("Content-Type"))
		bs, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, "10:b4aGVsbG8=", string(bs))
	}
	{
		u := httpSvr.URL + "?b64=1&j=jsonp_f2"
		resp, err := http.Get(u)
		require.NoError(t, err)

		defer func() {
			err = resp.Body.Close()
			require.NoError(t, err)
		}()

		assert.Equal(t, "text/plain; charset=UTF-8", resp.Header.Get("Content-Type"))

		bs, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "6:4world", string(bs))
	}

	wg.Wait()
}
