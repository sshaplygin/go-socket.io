package browser_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/browser"
)

var _ transport.Transport = (*browser.Transport)(nil)

func TestName(t *testing.T) {
	require.Equal(t, "websocket", (&browser.Transport{}).Name())
}

func TestAcceptAnswersBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	c, err := (&browser.Transport{}).Accept(w, httptest.NewRequest(http.MethodGet, "/socket.io/?EIO=3&transport=websocket", nil))
	require.Error(t, err)
	require.Nil(t, c)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
