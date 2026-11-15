package socketio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// handlerPeer serves one fake engine connection with a public Server whose
// records go to rec.
type handlerPeer struct {
	server  *Server
	fc      *fakeConn
	rec     *attrRecorder
	errs    chan error  // OnError calls of the namespaces registered by onError
	conns   chan Conn   // the Conn of each OnError call
	reasons chan string // OnDisconnect reasons of the root namespace
	// done is closed when the server has released the connection: its read
	// goroutine has returned and so has every disconnect callback, so each report
	// those goroutines raise has been delivered or logged.
	done chan struct{}
}

// newHandlerPeer builds the server; setup registers the handlers before the
// connection is served by start.
func newHandlerPeer(t *testing.T, setup func(*handlerPeer)) *handlerPeer {
	p := &handlerPeer{rec: newAttrRecorder(), fc: newFakeConn(t), errs: make(chan error, 16),
		conns: make(chan Conn, 16), reasons: make(chan string, 4), done: make(chan struct{})}
	p.server = NewServer(WithLogger(slog.New(p.rec)))
	p.server.OnDisconnect("/", func(_ Conn, reason string) { p.reasons <- reason })
	if setup != nil {
		setup(p)
	}
	go func() {
		defer close(p.done)
		p.server.serveConn(p.fc)
		// serveConn returns once the read goroutine runs; a rejected or already
		// released connection is no longer a holder.
		p.server.activeMu.Lock()
		c := p.server.holders[p.fc.ID()]
		p.server.activeMu.Unlock()
		if c != nil {
			<-c.released
		}
	}()
	t.Cleanup(func() {
		_ = p.fc.Close()
		recv(t, p.done, "connection release")
		_ = p.server.Close()
	})
	return p
}

// onError registers an OnError handler on nsp that records each call.
func (p *handlerPeer) onError(nsp string) {
	p.server.OnError(nsp, func(c Conn, err error) { p.conns <- c; p.errs <- err })
}

func (p *handlerPeer) send(t *testing.T, frame string) {
	t.Helper()
	recvSend(t, p.fc.reads, frame)
}

func recvSend(t *testing.T, ch chan<- string, frame string) {
	t.Helper()
	done := make(chan struct{})
	go func() { ch <- frame; close(done) }()
	recv(t, done, "send of "+frame)
}

// barrier proves that the connection still dispatches events.
func (p *handlerPeer) barrier(t *testing.T) {
	t.Helper()
	p.send(t, `2["barrier"]`)
	require.Equal(t, "2[\"barrier\"]\n", recv(t, p.fc.out, "barrier echo"))
}

func registerBarrier(s *Server) {
	s.OnEvent("/", "barrier", func(c Conn) { c.Emit("barrier") })
}

// requirePanic asserts that err is a recovered panic of value with a stack that
// names this test file.
func requirePanic(t *testing.T, err error, value string) {
	t.Helper()
	var panicked *PanicError
	require.ErrorAs(t, err, &panicked, "error %v", err)
	require.Equal(t, value, fmt.Sprint(panicked.Value))
	require.Contains(t, string(panicked.Stack), "handler_errors_test.go")
	require.Contains(t, err.Error(), "panic: "+value)
	if cause, ok := panicked.Value.(error); ok {
		require.ErrorIs(t, err, cause, "the panic value stays in the chain")
	}
}

// requirePanicRecord asserts that exactly one record reports the panic of value:
// a WARN "socketio: handler panic" with its stack, and no other record with the value.
func requirePanicRecord(t *testing.T, rec *attrRecorder, value string) {
	t.Helper()
	var found []map[string]string
	for _, m := range rec.since(0) {
		if strings.Contains(m["err"], value) && m["level"] != slog.LevelDebug.String() {
			found = append(found, m)
		}
	}
	require.Len(t, found, 1, "records %v", rec.since(0))
	require.Equal(t, "socketio: handler panic", found[0]["msg"])
	require.Equal(t, "WARN", found[0]["level"])
	require.Contains(t, found[0]["err"], "panic: "+value)
	require.Contains(t, found[0]["stack"], "handler_errors_test.go")
}

// fieldError is an error type with an exported field.
type fieldError struct{ Detail string }

func (e *fieldError) Error() string { return "field error: " + e.Detail }

func TestUntypedErrorReturnIsAcknowledgedAndReported(t *testing.T) {
	failure := errors.New("bad thing")
	p := newHandlerPeer(t, func(p *handlerPeer) {
		p.onError("/")
		registerBarrier(p.server)
		p.server.OnEvent("/", "fail", func(Conn) error { return failure })
		p.server.OnEvent("/", "ok", func(Conn) error { return nil })
		p.server.OnEvent("/", "pair", func(_ Conn, fail bool) (string, error) {
			if fail {
				return "", failure
			}
			return "done", nil
		})
		p.server.OnEvent("/", "remote", func(Conn) error { return &RemoteError{Code: "denied", Message: "no"} })
		p.server.OnEvent("/", "concrete", func(_ Conn, fail bool) *fieldError {
			if fail {
				return &fieldError{Detail: "disk"}
			}
			return nil
		})
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))

	p.send(t, `21["fail"]`)
	require.Equal(t, "31[{\"code\":\"handler_error\",\"message\":\"bad thing\"}]\n", recv(t, p.fc.out, "error ACK"))
	require.ErrorIs(t, recv(t, p.errs, "OnError of the returned error"), failure)
	require.Equal(t, "/", recv(t, p.conns, "OnError Conn").Namespace())

	p.send(t, `22["ok"]`)
	require.Equal(t, "32[null]\n", recv(t, p.fc.out, "nil error ACK"))

	p.send(t, `23["pair",true]`)
	require.Equal(t, "33[\"\",{\"code\":\"handler_error\",\"message\":\"bad thing\"}]\n", recv(t, p.fc.out, "positional error ACK"))
	require.ErrorIs(t, recv(t, p.errs, "OnError of the second result"), failure)
	recv(t, p.conns, "OnError Conn")

	p.send(t, `24["pair",false]`)
	require.Equal(t, "34[\"done\",null]\n", recv(t, p.fc.out, "success ACK"))

	p.send(t, `25["remote"]`)
	require.Equal(t, "35[{\"code\":\"denied\",\"message\":\"no\"}]\n", recv(t, p.fc.out, "RemoteError ACK"))
	var remote *RemoteError
	require.ErrorAs(t, recv(t, p.errs, "OnError of the RemoteError"), &remote)
	recv(t, p.conns, "OnError Conn")

	// A result declared with a concrete error type is a plain value: JSON-encoded
	// by its fields and not reported.
	p.send(t, `26["concrete",true]`)
	require.Equal(t, "36[{\"Detail\":\"disk\"}]\n", recv(t, p.fc.out, "concrete error type ACK"))
	p.send(t, `27["concrete",false]`)
	require.Equal(t, "37[null]\n", recv(t, p.fc.out, "nil concrete error type ACK"))

	// Without an ACK id the error is still reported, and nothing is written.
	p.send(t, `2["fail"]`)
	require.ErrorIs(t, recv(t, p.errs, "OnError without ACK"), failure)
	recv(t, p.conns, "OnError Conn")
	p.barrier(t)
	require.Empty(t, p.errs)
}

func TestUntypedErrorReturnWithoutOnErrorIsLogged(t *testing.T) {
	p := newHandlerPeer(t, func(p *handlerPeer) {
		registerBarrier(p.server)
		p.server.OnEvent("/", "fail", func(Conn) error { return errors.New("bad thing") })
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `2["fail"]`)
	p.barrier(t)
	records := only(p.rec.since(0), "socketio: unhandled error")
	require.Len(t, records, 1)
	require.Equal(t, "WARN", records[0]["level"])
	require.Equal(t, "bad thing", records[0]["err"])
}

func TestUntypedErrorReturnOfEveryListenerIsReported(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	p := newHandlerPeer(t, func(p *handlerPeer) {
		p.onError("/")
		registerBarrier(p.server)
		p.server.Of("/").On("multi", func(Conn) error { return first })
		p.server.Of("/").On("multi", func(Conn) error { return second })
		// An AckFunc owns the acknowledgement; its error result is still reported.
		p.server.OnEvent("/", "owned", func(_ Conn, ack AckFunc) error {
			ack("mine")
			return first
		})
		p.server.OnEvent("/", "disconnecting", func(Conn, string) error { return second })
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `21["multi"]`)
	require.Equal(t, "31[{\"code\":\"handler_error\",\"message\":\"first\"}]\n", recv(t, p.fc.out, "first listener's ACK"))
	require.ErrorIs(t, recv(t, p.errs, "first listener"), first)
	require.ErrorIs(t, recv(t, p.errs, "second listener"), second)
	p.send(t, `22["owned"]`)
	require.Equal(t, "32[\"mine\"]\n", recv(t, p.fc.out, "AckFunc ACK"))
	require.ErrorIs(t, recv(t, p.errs, "AckFunc handler error"), first)
	p.barrier(t)
	p.send(t, `1`)
	require.ErrorIs(t, recv(t, p.errs, "disconnecting listener error"), second)
	require.Equal(t, clientDisconnectMsg, recv(t, p.reasons, "OnDisconnect"))
}

func TestHandlerPanicsAreReportedOnce(t *testing.T) {
	type outcome struct {
		wire   string // the frame the peer receives after the trigger, if any
		reason string // OnDisconnect reason, if the connection closes
	}
	cases := []struct {
		name    string
		setup   func(*handlerPeer)
		trigger func(*testing.T, *handlerPeer)
		want    outcome
	}{
		{
			name: "OnEvent",
			setup: func(p *handlerPeer) {
				p.server.OnEvent("/", "boom", func(Conn) string { panic("event boom") })
			},
			trigger: func(t *testing.T, p *handlerPeer) { p.send(t, `21["boom"]`) },
			want:    outcome{reason: "forced close"},
		},
		{
			name: "OnEvent error value",
			setup: func(p *handlerPeer) {
				p.server.OnEvent("/", "boom", func(Conn) { panic(errors.New("event error boom")) })
			},
			trigger: func(t *testing.T, p *handlerPeer) { p.send(t, `2["boom"]`) },
			want:    outcome{reason: "forced close"},
		},
		{
			name: "typed handler",
			setup: func(p *handlerPeer) {
				_ = NewAckEvent[int, int]("typed").Handle(p.server.Of("/"), func(context.Context, Conn, int) (int, error) {
					panic("typed boom")
				})
			},
			trigger: func(t *testing.T, p *handlerPeer) { p.send(t, `21["typed",1]`) },
			want:    outcome{wire: "31[{\"code\":\"handler_panic\",\"message\":\"internal error\"}]\n"},
		},
		{
			name: "event middleware",
			setup: func(p *handlerPeer) {
				p.server.OnConnect("/", func(c Conn) error {
					c.Use(func(event string, _ []interface{}) error {
						if event == "guarded" {
							panic("middleware boom")
						}
						return nil
					})
					return nil
				})
				p.server.OnEvent("/", "guarded", func(Conn) {})
			},
			trigger: func(t *testing.T, p *handlerPeer) { p.send(t, `2["guarded"]`) },
			want:    outcome{wire: "4\"internal error\"\n"},
		},
		{
			name: "ACK callback",
			setup: func(p *handlerPeer) {
				p.server.OnEvent("/", "ask", func(c Conn) {
					c.Emit("question", func(string) { panic("ack boom") })
				})
			},
			trigger: func(t *testing.T, p *handlerPeer) {
				p.send(t, `2["ask"]`)
				id := regexp.MustCompile(`^2(\d+)\["question"\]\n$`).FindStringSubmatch(recv(t, p.fc.out, "question"))
				require.Len(t, id, 2)
				p.send(t, "3"+id[1]+`["answer"]`)
			},
			want: outcome{reason: "forced close"},
		},
		{
			name: "disconnecting listener",
			setup: func(p *handlerPeer) {
				p.server.OnEvent("/", "disconnecting", func(Conn, string) { panic("disconnecting boom") })
			},
			trigger: func(t *testing.T, p *handlerPeer) { p.send(t, `1`) },
			want:    outcome{reason: clientDisconnectMsg},
		},
		{
			name: "after Close",
			setup: func(p *handlerPeer) {
				p.server.OnEvent("/", "boom", func(c Conn) {
					_ = c.Close()
					panic("closing boom")
				})
			},
			trigger: func(t *testing.T, p *handlerPeer) { p.send(t, `2["boom"]`) },
			want:    outcome{reason: "server namespace disconnect"},
		},
	}
	values := map[string]string{"OnEvent": "event boom", "OnEvent error value": "event error boom",
		"typed handler": "typed boom", "event middleware": "middleware boom", "ACK callback": "ack boom",
		"disconnecting listener": "disconnecting boom", "after Close": "closing boom"}
	for _, tc := range cases {
		for _, withOnError := range []bool{true, false} {
			name := tc.name + "/without OnError"
			if withOnError {
				name = tc.name + "/with OnError"
			}
			t.Run(name, func(t *testing.T) {
				value := values[tc.name]
				p := newHandlerPeer(t, func(p *handlerPeer) {
					if withOnError {
						p.onError("/")
					}
					registerBarrier(p.server)
					tc.setup(p)
				})
				require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
				tc.trigger(t, p)
				if tc.want.wire != "" {
					require.Equal(t, tc.want.wire, recv(t, p.fc.out, "reply"))
				}
				if withOnError {
					requirePanic(t, recv(t, p.errs, "OnError of the panic"), value)
				}
				if tc.want.reason != "" {
					require.Equal(t, tc.want.reason, recv(t, p.reasons, "OnDisconnect"))
					// A panic after Close is reported once the handler returns, after
					// OnDisconnect: wait for the read goroutine before counting reports.
					// A client's namespace disconnect leaves the engine connection open.
					_ = p.fc.Close()
					recv(t, p.done, "connection release")
				} else {
					p.barrier(t) // the connection stays open
				}
				if withOnError {
					require.Empty(t, p.errs, "a second report")
					require.Empty(t, only(p.rec.since(0), "socketio: handler panic"))
				} else {
					requirePanicRecord(t, p.rec, value)
				}
				for _, frame := range drain(p.fc.out) {
					require.NotContains(t, frame, value, "the panic value reached the peer")
				}
			})
		}
	}
}

func TestConnectPanicsAreReportedOnce(t *testing.T) {
	for _, tc := range []struct {
		name, nsp string
		setup     func(s *Server, nsp string)
	}{
		{"root OnConnect", "/", func(s *Server, nsp string) {
			s.OnConnect(nsp, func(Conn) error { panic("connect boom") })
		}},
		{"root middleware", "/", func(s *Server, nsp string) {
			s.Use(nsp, func(Conn) error { panic("connect boom") })
		}},
		{"namespace OnConnect", "/chat", func(s *Server, nsp string) {
			s.OnConnect(nsp, func(Conn) error { panic("connect boom") })
		}},
		{"namespace middleware", "/chat", func(s *Server, nsp string) {
			s.Use(nsp, func(Conn) error { panic("connect boom") })
		}},
	} {
		for _, withOnError := range []bool{true, false} {
			name := tc.name + "/without OnError"
			if withOnError {
				name = tc.name + "/with OnError"
			}
			t.Run(name, func(t *testing.T) {
				p := newHandlerPeer(t, func(p *handlerPeer) {
					if withOnError {
						p.onError(tc.nsp)
					}
					registerBarrier(p.server)
					tc.setup(p.server, tc.nsp)
				})
				if tc.nsp == "/" {
					require.Equal(t, "4\"internal error\"\n", recv(t, p.fc.out, "root ERROR"))
				} else {
					require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
					p.send(t, "0/chat")
					require.Equal(t, "4/chat,\"internal error\"\n", recv(t, p.fc.out, "namespace ERROR"))
					p.barrier(t) // the root namespace stays connected
				}
				if withOnError {
					err := recv(t, p.errs, "OnError of the panic")
					requirePanic(t, err, "connect boom")
					conn := recv(t, p.conns, "OnError Conn")
					if tc.nsp == "/" {
						require.Nil(t, conn)
					} else {
						require.False(t, conn.Connected())
					}
					require.Empty(t, p.errs, "a second report")
				} else {
					requirePanicRecord(t, p.rec, "connect boom")
				}
			})
		}
	}
}

func TestDisconnectPanicIsReportedOnce(t *testing.T) {
	for _, withOnError := range []bool{true, false} {
		t.Run(map[bool]string{true: "with OnError", false: "without OnError"}[withOnError], func(t *testing.T) {
			p := newHandlerPeer(t, func(p *handlerPeer) {
				if withOnError {
					p.onError("/chat")
				}
				p.server.OnDisconnect("/chat", func(Conn, string) { panic("disconnect boom") })
				p.server.OnConnect("/chat", func(Conn) error { return nil })
			})
			require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
			p.send(t, "0/chat")
			require.Equal(t, "0/chat,", recv(t, p.fc.out, "namespace CONNECT"))
			p.send(t, "1/chat,")
			if withOnError {
				requirePanic(t, recv(t, p.errs, "OnError of the panic"), "disconnect boom")
				recv(t, p.conns, "OnError Conn")
			}
			// The connection keeps serving the root namespace and the namespace can
			// rejoin; the read goroutine has finished the disconnect by then.
			p.send(t, "0/chat")
			require.Equal(t, "0/chat,", recv(t, p.fc.out, "namespace CONNECT again"))
			if withOnError {
				require.Empty(t, p.errs, "a second report")
				require.Empty(t, only(p.rec.since(0), "socketio: handler panic"))
			} else {
				requirePanicRecord(t, p.rec, "disconnect boom")
			}
		})
	}
}

func TestOnErrorPanicIsLoggedWithStack(t *testing.T) {
	p := newHandlerPeer(t, func(p *handlerPeer) {
		registerBarrier(p.server)
		p.server.OnEvent("/", "fail", func(Conn) error { return errors.New("bad thing") })
		p.server.OnError("/", func(Conn, error) { panic("error handler boom") })
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `2["fail"]`)
	p.barrier(t)
	records := only(p.rec.since(0), "socketio: error handler panic")
	require.Len(t, records, 1, "records %v", p.rec.since(0))
	require.Equal(t, "WARN", records[0]["level"])
	require.Contains(t, records[0]["err"], "panic: error handler boom")
	require.Contains(t, records[0]["stack"], "handler_errors_test.go")
}

func TestErrorResultsBeforeAListenerPanicAreReported(t *testing.T) {
	first := errors.New("first")
	p := newHandlerPeer(t, func(p *handlerPeer) {
		p.onError("/")
		p.server.Of("/").On("multi", func(Conn) error { return first })
		p.server.Of("/").On("multi", func(Conn) error { panic("listener boom") })
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `21["multi"]`)
	require.ErrorIs(t, recv(t, p.errs, "the first listener's error"), first)
	requirePanic(t, recv(t, p.errs, "the second listener's panic"), "listener boom")
	require.Equal(t, "forced close", recv(t, p.reasons, "OnDisconnect"))
	recv(t, p.done, "connection release")
	require.Empty(t, p.errs, "a further report")
	for _, frame := range drain(p.fc.out) {
		require.NotRegexp(t, `^3`, frame, "an ACK after the panic")
	}
}

// A handler that calls Close and then panics has its panic reported when it
// returns, after the OnDisconnect that Close ran.
func TestPanicAfterCloseIsReportedAfterOnDisconnect(t *testing.T) {
	order := make(chan string, 4)
	p := newHandlerPeer(t, func(p *handlerPeer) {
		p.server.OnDisconnect("/", func(Conn, string) { order <- "OnDisconnect" })
		p.server.OnError("/", func(_ Conn, err error) { order <- "OnError: " + err.Error() })
		p.server.OnEvent("/", "boom", func(c Conn) {
			_ = c.Close()
			panic("closing boom")
		})
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `2["boom"]`)
	recv(t, p.done, "connection release")
	require.Equal(t, []string{"OnDisconnect", "OnError: panic: closing boom"}, drain(order))
}

// nilPointerError has a pointer receiver that dereferences it, so the Error
// method of a typed nil of it panics.
type nilPointerError struct{ msg string }

func (e *nilPointerError) Error() string { return e.msg }

func TestTypedNilErrorResultCountsAsNil(t *testing.T) {
	var typedNil *nilPointerError
	p := newHandlerPeer(t, func(p *handlerPeer) {
		p.onError("/")
		p.onError("/chat")
		registerBarrier(p.server)
		p.server.OnConnect("/", func(c Conn) error {
			c.Use(func(string, []interface{}) error { return typedNil })
			return typedNil
		})
		p.server.OnEvent("/", "single", func(Conn) error { return typedNil })
		p.server.OnEvent("/", "pair", func(Conn) (string, error) { return "done", typedNil })
		require.NoError(t, NewAckEvent[int, int]("typed").Handle(p.server.Of("/"), func(context.Context, Conn, int) (int, error) {
			return 7, typedNil
		}))
		p.server.Use("/chat", func(Conn) error { return typedNil })
		p.server.OnConnect("/chat", func(Conn) error { return typedNil })
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `21["single"]`)
	require.Equal(t, "31[null]\n", recv(t, p.fc.out, "single result ACK"))
	p.send(t, `22["pair"]`)
	require.Equal(t, "32[\"done\",null]\n", recv(t, p.fc.out, "pair ACK"))
	p.send(t, `23["typed",1]`)
	require.Equal(t, "33[null,7]\n", recv(t, p.fc.out, "typed ACK"))
	p.send(t, "0/chat")
	require.Equal(t, "0/chat,", recv(t, p.fc.out, "namespace CONNECT"))
	p.barrier(t)
	require.Empty(t, p.errs)
	require.Empty(t, only(p.rec.since(0), "socketio: unhandled error"))
}

func TestConnectPanicAfterCloseIsReportedOnce(t *testing.T) {
	for name, nsp := range map[string]string{"root": "/", "namespace": "/chat"} {
		for _, withOnError := range []bool{true, false} {
			t.Run(name+map[bool]string{true: "/with OnError", false: "/without OnError"}[withOnError], func(t *testing.T) {
				p := newHandlerPeer(t, func(p *handlerPeer) {
					if withOnError {
						p.onError(nsp)
					}
					p.server.OnConnect(nsp, func(c Conn) error {
						_ = c.Close()
						panic("connect boom")
					})
				})
				if nsp != "/" {
					require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
					p.send(t, "0"+nsp)
				}
				if withOnError {
					requirePanic(t, recv(t, p.errs, "OnError of the panic"), "connect boom")
				}
				recv(t, p.done, "connection release")
				if withOnError {
					require.Empty(t, p.errs, "a second report")
					require.Empty(t, only(p.rec.since(0), "socketio: handler panic"))
				} else {
					requirePanicRecord(t, p.rec, "connect boom")
				}
				for _, frame := range drain(p.fc.out) {
					require.NotContains(t, frame, "connect boom", "the panic value reached the peer")
				}
			})
		}
	}
}

func TestNamespaceMatchPanicIsReported(t *testing.T) {
	p := newHandlerPeer(t, func(p *handlerPeer) {
		registerBarrier(p.server)
		p.server.OfFunc(func(string, url.Values) bool { panic("match boom") })
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, "0/dyn")
	require.Equal(t, "4/dyn,\"internal error\"\n", recv(t, p.fc.out, "namespace ERROR"))
	p.barrier(t) // the root namespace stays connected
	requirePanicRecord(t, p.rec, "match boom")
	require.Equal(t, "/dyn", only(p.rec.since(0), "socketio: handler panic")[0]["nsp"])
}

func TestReservedLocalEmitPanicKeepsTheConnection(t *testing.T) {
	for _, withOnError := range []bool{true, false} {
		t.Run(map[bool]string{true: "with OnError", false: "without OnError"}[withOnError], func(t *testing.T) {
			p := newHandlerPeer(t, func(p *handlerPeer) {
				if withOnError {
					p.onError("/")
				}
				registerBarrier(p.server)
				p.server.OnEvent("/", "error", func(Conn, string) { panic("local boom") })
				p.server.OnEvent("/", "emit", func(c Conn) {
					c.Emit("error", "local")
					c.Emit("after")
				})
			})
			require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
			p.send(t, `2["emit"]`)
			if withOnError {
				requirePanic(t, recv(t, p.errs, "OnError of the panic"), "local boom")
			}
			require.Equal(t, "2[\"after\"]\n", recv(t, p.fc.out, "the Emit after the panic"))
			p.barrier(t)
			if withOnError {
				require.Empty(t, p.errs, "a second report")
			} else {
				requirePanicRecord(t, p.rec, "local boom")
			}
			require.Empty(t, p.reasons, "the connection closed")
		})
	}
}

func TestErrorResultAfterCloseIsLoggedAtDebug(t *testing.T) {
	p := newHandlerPeer(t, func(p *handlerPeer) {
		p.onError("/")
		p.server.OnEvent("/", "fail", func(c Conn) error {
			_ = c.Close()
			return errors.New("after close failure")
		})
	})
	require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	p.send(t, `2["fail"]`)
	require.Equal(t, "server namespace disconnect", recv(t, p.reasons, "OnDisconnect"))
	recv(t, p.done, "connection release") // the handler has returned its error
	records := only(p.rec.since(0), "socketio: error after close")
	require.Len(t, records, 1, "records %v", p.rec.since(0))
	require.Equal(t, "DEBUG", records[0]["level"])
	require.Equal(t, "after close failure", records[0]["err"])
	require.Empty(t, p.errs, "OnError after the close started")
}

// brokenError is a non-nil error whose Error method panics.
type brokenError struct{}

func (brokenError) Error() string { panic("error method boom") }

// An error result whose Error method panics is a panic of the callback that
// returned it: reported once with the stack, answered with the fixed text, and
// the connection keeps dispatching.
func TestErrorMethodPanicIsReportedAsPanic(t *testing.T) {
	const panicAck = "[{\"code\":\"handler_panic\",\"message\":\"internal error\"}]\n"
	for _, withOnError := range []bool{true, false} {
		t.Run(map[bool]string{true: "with OnError", false: "without OnError"}[withOnError], func(t *testing.T) {
			p := newHandlerPeer(t, func(p *handlerPeer) {
				if withOnError {
					p.onError("/")
					p.onError("/chat")
				}
				registerBarrier(p.server)
				p.server.OnConnect("/", func(c Conn) error {
					c.Use(func(event string, _ []interface{}) error {
						if event == "guarded" {
							return brokenError{}
						}
						return nil
					})
					return nil
				})
				p.server.OnEvent("/", "guarded", func(Conn) {})
				p.server.OnEvent("/", "single", func(Conn) error { return brokenError{} })
				p.server.OnEvent("/", "pair", func(Conn) (string, error) { return "done", brokenError{} })
				require.NoError(t, NewAckEvent[int, int]("typed").Handle(p.server.Of("/"), func(context.Context, Conn, int) (int, error) {
					return 0, brokenError{}
				}))
				p.server.OnConnect("/chat", func(Conn) error { return brokenError{} })
			})
			require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
			reports := 0
			step := func(frame, want string) {
				t.Helper()
				p.send(t, frame)
				require.Equal(t, want, recv(t, p.fc.out, "reply to "+frame))
				reports++
				if withOnError {
					err := recv(t, p.errs, "OnError for "+frame)
					requirePanic(t, err, "error method boom")
					require.Contains(t, err.Error(), "error method: ")
				}
			}
			step(`21["single"]`, "31"+panicAck)
			step(`22["pair"]`, "32[\"done\",{\"code\":\"handler_panic\",\"message\":\"internal error\"}]\n")
			step(`23["typed",1]`, "33"+panicAck)
			step(`2["guarded"]`, "4\"internal error\"\n")
			step("0/chat", "4/chat,\"internal error\"\n")
			p.barrier(t)
			require.Empty(t, p.errs, "a second report")
			require.Empty(t, p.reasons, "the connection closed")
			if !withOnError {
				records := only(p.rec.since(0), "socketio: handler panic")
				require.Len(t, records, reports, "records %v", p.rec.since(0))
				for _, record := range records {
					require.Equal(t, "WARN", record["level"])
					require.Contains(t, record["stack"], "handler_errors_test.go")
				}
				require.Empty(t, only(p.rec.since(0), "socketio: unhandled error"))
			}
		})
	}
}

// The error results of disconnecting listeners, which only run after the close
// started, and of listeners run by a local Emit are reported, not logged at DEBUG
// as the error results of incoming event handlers are.
func TestDisconnectingErrorResultIsReportedDuringClose(t *testing.T) {
	failure, local := errors.New("disconnecting failure"), errors.New("local failure")
	closes := []struct {
		name   string
		close  func(*testing.T, *handlerPeer)
		reason string
		local  bool // an "error" listener is run by a local Emit after the close started
	}{
		{name: "engine connection close", close: func(_ *testing.T, p *handlerPeer) { _ = p.fc.Close() }, reason: "forced close"},
		{name: "Conn.Close", close: func(t *testing.T, p *handlerPeer) { p.send(t, `2["close"]`) }, reason: "server namespace disconnect", local: true},
		{name: "Server.Close", close: func(_ *testing.T, p *handlerPeer) { _ = p.server.Close() }, reason: "forced close"},
	}
	for _, tc := range closes {
		for _, withOnError := range []bool{true, false} {
			t.Run(tc.name+map[bool]string{true: "/with OnError", false: "/without OnError"}[withOnError], func(t *testing.T) {
				closing := make(chan bool, 2)
				p := newHandlerPeer(t, func(p *handlerPeer) {
					if withOnError {
						p.onError("/")
					}
					p.server.OnEvent("/", "disconnecting", func(c Conn, _ string) error {
						closing <- isDone(c.(*namespaceConn).connection.closing)
						return failure
					})
					p.server.OnEvent("/", "error", func(c Conn, _ string) error {
						closing <- isDone(c.(*namespaceConn).connection.closing)
						return local
					})
					p.server.OnEvent("/", "close", func(c Conn) {
						_ = c.Close()
						c.Emit("error", "local")
					})
				})
				require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
				tc.close(t, p)
				want := []error{failure}
				if tc.local {
					want = append(want, local)
				}
				var got []error
				for range want {
					require.True(t, recv(t, closing, "listener"), "the close had not started")
					if withOnError {
						got = append(got, recv(t, p.errs, "OnError of a listener error result"))
					}
				}
				require.Equal(t, tc.reason, recv(t, p.reasons, "OnDisconnect"))
				// The local Emit of the close handler reports after OnDisconnect, on
				// the read goroutine.
				recv(t, p.done, "connection release")
				records := only(p.rec.since(0), "socketio: unhandled error")
				if withOnError {
					require.ElementsMatch(t, want, got)
					require.Empty(t, p.errs, "a second report")
					require.Empty(t, records)
				} else {
					require.Len(t, records, len(want), "records %v", p.rec.since(0))
					for _, record := range records {
						require.Equal(t, "WARN", record["level"])
						require.Contains(t, []string{failure.Error(), local.Error()}, record["err"])
					}
				}
				require.Empty(t, only(p.rec.since(0), "socketio: error after close"))
			})
		}
	}
}
