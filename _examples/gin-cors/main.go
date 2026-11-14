package main

import (
	"context"
	"flag"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func main() {
	addr := flag.String("addr", ":8000", "HTTP listen address")
	allowOrigin := flag.String("allow-origin", "http://localhost:3000", "browser origin allowed besides the server's own")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr, *allowOrigin); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, addr, allowOrigin string) error {
	server, router := newHandler(allowOrigin)
	return serve(ctx, server, &http.Server{Addr: addr, Handler: router})
}

// originChecker accepts allowOrigin, the server's own origin and requests without an
// Origin, which non-browser clients such as the Go and Node clients send. Browsers
// always send Origin on websocket handshakes and cross-origin XHRs.
func originChecker(allowOrigin string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" || origin == allowOrigin {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && strings.EqualFold(u.Host, r.Host)
	}
}

// newHandler leaves CORS to the transports' origin checks. Polling reflects an accepted
// Origin with credentials, as the default socket.io-client XHR requires, and sends no CORS
// headers to other origins; websocket refuses other origins with HTTP 403. WithCORS(false)
// keeps CORS headers off the error for an unknown transport= value, which no transport
// check handles. Middleware in front of the handler must keep the request's Origin header
// and must not set the Access-Control-Allow-Origin or -Credentials headers itself; see
// docs/PROTOCOL.md#cors.
func newHandler(allowOrigin string) (*socketio.Server, http.Handler) {
	checkOrigin := originChecker(allowOrigin)
	server := socketio.NewServer(socketio.WithEngineOptions(
		engineio.WithCORS(false),
		engineio.WithTransports(
			&polling.Transport{CheckOrigin: checkOrigin},
			&websocket.Transport{CheckOrigin: checkOrigin},
		),
	))

	registerChat(server)

	router := gin.New()
	handler := gin.WrapH(server)
	router.GET("/socket.io/*any", handler)
	router.POST("/socket.io/*any", handler)
	// The preflight of a binary polling POST (Content-Type application/octet-stream).
	router.OPTIONS("/socket.io/*any", handler)
	router.StaticFS("/public", http.Dir("../asset"))

	return server, router
}
