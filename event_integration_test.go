package socketio_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
)

func typedReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("typed event did not complete")
		var zero T
		return zero
	}
}

func typedPair(t *testing.T, tr transport.Transport, configure func(*socketio.Server, *client.Client)) (*socketio.Server, *client.Client, socketio.Conn, client.Conn) {
	t.Helper()
	return typedPairOptions(t, tr, configure)
}

func typedPairOptions(t *testing.T, tr transport.Transport, configure func(*socketio.Server, *client.Client), options ...engineio.Option) (*socketio.Server, *client.Client, socketio.Conn, client.Conn) {
	t.Helper()
	srv := socketio.NewServer(socketio.WithEngineOptions(options...))
	serverReady := make(chan socketio.Conn, 1)
	srv.OnConnect("/chat", func(c socketio.Conn) error { serverReady <- c; return nil })
	httpServer := httptest.NewServer(srv)
	served := make(chan error, 1)
	t.Cleanup(func() {
		closeErr := srv.Close()
		httpServer.Close()
		serveErr := typedReceive(t, served)
		require.NoError(t, closeErr)
		require.NoError(t, serveErr)
	})
	go func() { served <- srv.Serve() }()
	cl, err := client.New(httpServer.URL+"/chat", client.WithTransports(tr), client.WithReconnection(false))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cl.Close()) })
	clientReady := make(chan client.Conn, 1)
	cl.OnConnect(func(c client.Conn) error { clientReady <- c; return nil })
	configure(srv, cl)
	require.NoError(t, cl.Connect())
	return srv, cl, typedReceive(t, serverReady), typedReceive(t, clientReady)
}

type typedPayload struct {
	Signed   int64
	Unsigned uint64
	Binary   parser.Buffer
	Nested   map[string]parser.Buffer
}

func typedValue() typedPayload {
	return typedPayload{Signed: 9007199254740993, Unsigned: 18446744073709551615, Binary: parser.Buffer{Data: []byte("binary")}, Nested: map[string]parser.Buffer{"nested": {Data: []byte{0, 1, 255}}}}
}

// Covers T1
// Covers T2
func TestTypedEventAcrossServerClientAndBroadcast(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			descriptor := socketio.NewEvent[typedPayload]("typed-payload")
			serverValues, clientValues := make(chan typedPayload, 2), make(chan typedPayload, 2)
			srv, cl, serverConn, clientConn := typedPair(t, tr, func(srv *socketio.Server, cl *client.Client) {
				require.NoError(t, descriptor.Handle(srv.Of("/chat"), func(_ context.Context, c socketio.Conn, value typedPayload) error {
					require.Equal(t, "/chat", c.Namespace())
					serverValues <- value
					return nil
				}))
				require.NoError(t, descriptor.HandleClient(cl, func(_ context.Context, to socketio.Endpoint, value typedPayload) error {
					c, ok := to.(client.Conn)
					require.True(t, ok)
					require.Equal(t, "/chat", c.Namespace())
					clientValues <- value
					return nil
				}))
			})
			value := typedValue()
			require.Equal(t, "typed-payload", descriptor.Name())
			require.NoError(t, descriptor.Emit(context.Background(), cl, value))
			require.Equal(t, value, typedReceive(t, serverValues))
			require.NoError(t, descriptor.Emit(context.Background(), serverConn, value))
			require.Equal(t, value, typedReceive(t, clientValues))
			require.NoError(t, descriptor.EmitTo(context.Background(), srv.Of("/chat").To(serverConn.ID()), value))
			require.Equal(t, value, typedReceive(t, clientValues))
			require.True(t, clientConn.Connected())
		})
	}
}

// Covers T3
func TestTypedAcknowledgementsInBothDirections(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			descriptor := socketio.NewAckEvent[typedPayload, typedPayload]("typed-echo")
			_, cl, serverConn, _ := typedPair(t, tr, func(srv *socketio.Server, cl *client.Client) {
				require.NoError(t, descriptor.Handle(srv.Of("/chat"), func(_ context.Context, _ socketio.Conn, value typedPayload) (typedPayload, error) { return value, nil }))
				require.NoError(t, descriptor.HandleClient(cl.Socket("/chat"), func(_ context.Context, _ socketio.Endpoint, value typedPayload) (typedPayload, error) {
					return value, nil
				}))
			})
			require.Equal(t, "typed-echo", descriptor.Name())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for _, to := range []socketio.Endpoint{cl.Socket("/chat"), serverConn} {
				value, err := descriptor.EmitWithAck(ctx, to, typedValue())
				require.NoError(t, err)
				require.Equal(t, typedValue(), value)
			}
		})
	}
}

func TestTypedHandlerFailuresKeepConnectionUsable(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			descriptor := socketio.NewAckEvent[string, string]("typed-errors")
			ordinary := socketio.NewEvent[string]("typed-failure")
			serverErrors, clientErrors := make(chan error, 16), make(chan error, 16)
			fail := func(value string) (string, error) {
				switch value {
				case "remote":
					return "", &socketio.RemoteError{Code: "refused", Message: "no"}
				case "error":
					return "", errors.New("handler failed")
				case "panic":
					panic("handler panicked")
				}
				return value, nil
			}
			_, cl, serverConn, clientConn := typedPair(t, tr, func(srv *socketio.Server, cl *client.Client) {
				srv.OnError("/chat", func(_ socketio.Conn, err error) { serverErrors <- err })
				cl.OnError(func(_ client.Conn, err error) { clientErrors <- err })
				require.NoError(t, descriptor.Handle(srv.Of("/chat"), func(_ context.Context, _ socketio.Conn, value string) (string, error) { return fail(value) }))
				require.NoError(t, descriptor.HandleClient(cl, func(_ context.Context, _ socketio.Endpoint, value string) (string, error) { return fail(value) }))
				require.NoError(t, ordinary.Handle(srv.Of("/chat"), func(_ context.Context, _ socketio.Conn, value string) error {
					if value == "panic" {
						panic("ordinary panic")
					}
					return errors.New("ordinary failure")
				}))
				require.NoError(t, ordinary.HandleClient(cl, func(_ context.Context, _ socketio.Endpoint, value string) error {
					if value == "panic" {
						panic("ordinary panic")
					}
					return errors.New("ordinary failure")
				}))
			})
			for _, to := range []socketio.Endpoint{cl, serverConn} {
				for _, test := range []struct{ value, code string }{{"remote", "refused"}, {"error", "handler_error"}, {"panic", "handler_panic"}} {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_, err := descriptor.EmitWithAck(ctx, to, test.value)
					cancel()
					var remote *socketio.RemoteError
					require.ErrorAs(t, err, &remote)
					require.Equal(t, test.code, remote.Code)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				value, err := descriptor.EmitWithAck(ctx, to, "alive")
				cancel()
				require.NoError(t, err)
				require.Equal(t, "alive", value)
			}
			// Identify ordinary failures independently from optional local ACK reports.
			for _, target := range []struct {
				to      socketio.Endpoint
				reports chan error
			}{{cl, serverErrors}, {serverConn, clientErrors}} {
				for _, test := range []struct{ value, message string }{{"error", "ordinary failure"}, {"panic", "ordinary panic"}} {
					require.NoError(t, ordinary.Emit(context.Background(), target.to, test.value))
					require.Eventually(t, func() bool {
						for len(target.reports) > 0 {
							if err := <-target.reports; strings.Contains(err.Error(), test.message) {
								return true
							}
						}
						return false
					}, time.Second, time.Millisecond)
				}
			}
			require.True(t, serverConn.Connected())
			require.True(t, clientConn.Connected())
		})
	}
}

func TestTypedHandlerContextsEndBeforeDisconnectCallbacks(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			descriptor := socketio.NewEvent[bool]("typed-context")
			serverContexts, clientContexts := make(chan context.Context, 1), make(chan context.Context, 1)
			serverCanceled, clientCanceled := make(chan bool, 1), make(chan bool, 1)
			var serverContext, clientContext context.Context
			_, cl, serverConn, clientConn := typedPair(t, tr, func(srv *socketio.Server, cl *client.Client) {
				require.NoError(t, descriptor.Handle(srv.Of("/chat"), func(ctx context.Context, _ socketio.Conn, _ bool) error {
					serverContext = ctx
					serverContexts <- ctx
					return nil
				}))
				require.NoError(t, descriptor.HandleClient(cl, func(ctx context.Context, _ socketio.Endpoint, _ bool) error {
					clientContext = ctx
					clientContexts <- ctx
					return nil
				}))
				srv.OnDisconnect("/chat", func(socketio.Conn, string) { serverCanceled <- serverContext != nil && serverContext.Err() != nil })
				cl.OnDisconnect(func(client.Conn, string) { clientCanceled <- clientContext != nil && clientContext.Err() != nil })
			})
			serverConn.SetContext("server application context")
			clientConn.SetContext("client application context")
			require.NoError(t, descriptor.Emit(context.Background(), cl, true))
			sc := typedReceive(t, serverContexts)
			require.NoError(t, descriptor.Emit(context.Background(), serverConn, true))
			cc := typedReceive(t, clientContexts)
			require.NoError(t, sc.Err())
			require.NoError(t, cc.Err())
			require.NoError(t, cl.Socket("/chat").Close())
			require.True(t, typedReceive(t, clientCanceled))
			require.True(t, typedReceive(t, serverCanceled))
			require.ErrorIs(t, sc.Err(), context.Canceled)
			require.ErrorIs(t, cc.Err(), context.Canceled)
			require.Equal(t, "server application context", serverConn.Context())
			require.Equal(t, "client application context", clientConn.Context())
		})
	}
}

func TestTypedAndLegacyRegistrationReplacement(t *testing.T) {
	descriptor := socketio.NewEvent[int]("typed-replacement")
	serverValues, clientValues := make(chan int, 1), make(chan int, 1)
	srv, cl, serverConn, _ := typedPair(t, websocket.Default, func(srv *socketio.Server, cl *client.Client) {
		serverHandler := func(_ context.Context, _ socketio.Conn, value int) error { serverValues <- value; return nil }
		clientHandler := func(_ context.Context, _ socketio.Endpoint, value int) error { clientValues <- value; return nil }
		require.NoError(t, descriptor.Handle(srv.Of("/chat"), serverHandler))
		require.ErrorIs(t, descriptor.Handle(srv.Of("/chat"), serverHandler), socketio.ErrDuplicateEvent)
		require.NoError(t, descriptor.HandleClient(cl, clientHandler))
		require.ErrorIs(t, descriptor.HandleClient(cl, clientHandler), socketio.ErrDuplicateEvent)
		srv.OnEvent("/chat", descriptor.Name(), func(_ socketio.Conn, value int) int { return value + 1 })
		cl.OnEvent(descriptor.Name(), func(_ client.Conn, value int) int { return value + 1 })
	})
	replies := make(chan int, 2)
	cl.Emit(descriptor.Name(), 41, func(value int) { replies <- value })
	require.Equal(t, 42, typedReceive(t, replies))
	serverConn.Emit(descriptor.Name(), 42, func(value int) { replies <- value })
	require.Equal(t, 43, typedReceive(t, replies))
	require.Empty(t, serverValues)
	require.Empty(t, clientValues)
	srv.Of("/chat").Off(descriptor.Name())
	cl.Socket("/chat").Off(descriptor.Name())
	require.NoError(t, descriptor.Handle(srv.Of("/chat"), func(_ context.Context, _ socketio.Conn, value int) error { serverValues <- value; return nil }))
	require.NoError(t, descriptor.HandleClient(cl, func(_ context.Context, _ socketio.Endpoint, value int) error { clientValues <- value; return nil }))
	require.NoError(t, descriptor.Emit(context.Background(), cl, 1))
	require.Equal(t, 1, typedReceive(t, serverValues))
	require.NoError(t, descriptor.Emit(context.Background(), serverConn, 2))
	require.Equal(t, 2, typedReceive(t, clientValues))
}

func TestTypedEngineCloseUnblocksLifetimeWait(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		for _, side := range []string{"server", "client"} {
			t.Run(tr.Name()+"/"+side, func(t *testing.T) {
				descriptor := socketio.NewEvent[bool]("typed-lifetime-wait")
				entered := make(chan struct{}, 1)
				stopped := make(chan error, 1)
				release := make(chan struct{})
				defer close(release)
				wait := func(ctx context.Context) {
					entered <- struct{}{}
					select {
					case <-ctx.Done():
						stopped <- ctx.Err()
					case <-release:
						stopped <- nil
					}
				}
				_, cl, serverConn, _ := typedPairOptions(t, tr, func(srv *socketio.Server, cl *client.Client) {
					if side == "server" {
						require.NoError(t, descriptor.Handle(srv.Of("/chat"), func(ctx context.Context, _ socketio.Conn, _ bool) error { wait(ctx); return nil }))
					} else {
						require.NoError(t, descriptor.HandleClient(cl, func(ctx context.Context, _ socketio.Endpoint, _ bool) error { wait(ctx); return nil }))
					}
				}, engineio.WithPingInterval(20*time.Millisecond), engineio.WithPingTimeout(time.Second))
				var to socketio.Endpoint = cl
				if side == "client" {
					to = serverConn
				}
				require.NoError(t, descriptor.Emit(context.Background(), to, true))
				typedReceive(t, entered)
				// Close only the engine. The dispatcher is inside wait, so its usual read
				// failure cleanup cannot cancel the handler context on its own.
				require.NoError(t, serverConn.EngineConn().Close())
				require.ErrorIs(t, typedReceive(t, stopped), context.Canceled)
			})
		}
	}
}
