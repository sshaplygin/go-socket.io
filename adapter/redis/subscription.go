package redis

import (
	"context"
	"time"

	"github.com/gomodule/redigo/redis"
)

// redisDialTimeout bounds the two dials that build a Redis broadcast, the
// publishing and the subscriber connection together, including AUTH and
// SELECT, and each later dial of the publishing pool. Registration holds the
// creation mutex for that time, and Server.Close waits for it; tests shorten
// it.
var redisDialTimeout = 10 * time.Second

// Backoff between attempts to reopen the subscriber connection after a
// receive error; tests shorten it.
var (
	redisReconnectMin = 100 * time.Millisecond
	redisReconnectMax = 5 * time.Second
)

// subscribe opens a subscriber connection to the broadcast pattern and the
// request and response channels, and closes it again if that fails. ctx bounds
// only the dial.
func (bc *broadcast) subscribe(ctx context.Context) (*redis.PubSubConn, error) {
	c, err := (*bc.dial.Load())(ctx)
	if err != nil {
		return nil, err
	}
	sub := &redis.PubSubConn{Conn: c}
	if err = sub.PSubscribe(bc.pattern); err == nil {
		err = sub.Subscribe(bc.reqChannel, bc.resChannel)
	}
	if err != nil {
		_ = sub.Close()
		return nil, err
	}
	return sub, nil
}

// dispatch handles the messages of the subscriber connection until the
// broadcast is closed. Malformed messages are skipped; after a receive error
// the subscriber connection is reopened.
func (bc *broadcast) dispatch() {
	sub := bc.sub
	delay := redisReconnectMin
	for {
		switch m := sub.Receive().(type) {
		case redis.Message:
			switch m.Channel {
			case bc.reqChannel:
				bc.onRequest(m.Data)
			case bc.resChannel:
				bc.onResponse(m.Data)
			default:
				_ = bc.onMessage(m.Channel, m.Data)
			}

		case redis.Subscription:
			if m.Count == 0 {
				return
			}
			// The server accepted the subscription, so the backoff starts over.
			delay = redisReconnectMin

		case error:
			_ = sub.Close()
			if sub, delay = bc.resubscribe(delay); sub == nil {
				return
			}
		}
	}
}

// resubscribe opens a new subscriber connection, waiting delay before the
// first attempt and doubling it up to redisReconnectMax after each attempt.
// It returns the connection and the delay for the next reconnect, so the
// backoff also grows when the server refuses each new subscription, or a
// nil connection once the broadcast is closed.
func (bc *broadcast) resubscribe(delay time.Duration) (*redis.PubSubConn, time.Duration) {
	for ; ; delay = min(2*delay, redisReconnectMax) {
		select {
		case <-bc.done:
			return nil, 0
		case <-time.After(delay):
		}

		sub, err := bc.subscribe(context.Background())
		if err != nil {
			continue
		}

		bc.subLock.Lock()
		defer bc.subLock.Unlock()
		select {
		case <-bc.done:
			_ = sub.Close()
			return nil, 0
		default:
			bc.sub = sub
			return sub, min(2*delay, redisReconnectMax)
		}
	}
}

// Close stops the dispatch goroutine and closes the connections.
func (bc *broadcast) Close() error {
	bc.subLock.Lock()
	defer bc.subLock.Unlock()
	select {
	case <-bc.done:
		return nil
	default:
	}

	close(bc.done)
	_ = bc.sub.Close()
	_ = bc.pub.Close()
	return nil
}
