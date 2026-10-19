package redis

import (
	"context"
	"github.com/gomodule/redigo/redis"
	"io"
	"sync"
	"testing"
	"time"
)

func TestDatabaseOptionPreserved(t *testing.T) {
	if got := getOptions(&options{DB: 3}).DB; got != 3 {
		t.Fatalf("database = %d, want 3", got)
	}
}

// A successful command write does not mean Redis registered the subscription.
func TestSubscribeWaitsForAcknowledgements(t *testing.T) {
	c := &silentSubscription{done: make(chan struct{})}
	bc := &broadcast{pattern: "socket.io#*", reqChannel: "socket.io-request#/#", resChannel: "socket.io-response#/#"}
	dial := func(context.Context) (redis.Conn, error) { return c, nil }
	bc.dial.Store(&dial)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	sub, err := bc.subscribe(ctx)
	if sub != nil {
		_ = sub.Close()
	}
	if err == nil {
		t.Fatal("subscription returned ready without Redis acknowledgements")
	}
}

type silentSubscription struct {
	done chan struct{}
	once sync.Once
}

func (c *silentSubscription) Close() error                                   { c.once.Do(func() { close(c.done) }); return nil }
func (c *silentSubscription) Err() error                                     { return nil }
func (c *silentSubscription) Do(string, ...interface{}) (interface{}, error) { return nil, nil }
func (c *silentSubscription) Send(string, ...interface{}) error              { return nil }
func (c *silentSubscription) Flush() error                                   { return nil }
func (c *silentSubscription) Receive() (interface{}, error)                  { <-c.done; return nil, io.EOF }
