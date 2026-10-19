package polling

import (
	"context"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

// Covers: G11
func TestCanceledPollFailsTransportBeforeNextWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "http://localhost/?transport=polling", nil).WithContext(ctx)
	c := newServerConn(Default, req)
	defer c.Close()
	done := make(chan struct{})
	go func() { defer close(done); c.ServeHTTP(httptest.NewRecorder(), req) }()
	require.Eventually(t, c.Writable, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled poll retained handler")
	}
	_, err := c.NextWriter(eioparser.String, eioparser.MESSAGE)
	require.ErrorIs(t, err, context.Canceled)
	_, _, _, err = c.NextReader()
	require.ErrorIs(t, err, context.Canceled)
}
