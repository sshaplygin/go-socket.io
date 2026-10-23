package redis

import (
	"context"
	"time"

	"github.com/gomodule/redigo/redis"
)

// redisDialTimeout is the default construction, reconnect and close timeout.
const redisDialTimeout = 10 * time.Second

func (bc *broadcast) constructionTimeout() time.Duration {
	if bc.dialTimeout > 0 {
		return bc.dialTimeout
	}
	return redisDialTimeout
}

// Backoff between attempts to reopen the subscriber connection after a
// receive error; tests shorten it.
var (
	redisReconnectMin = 100 * time.Millisecond
	redisReconnectMax = 5 * time.Second
)

// subscribe opens a subscriber connection to the broadcast pattern and the
// request and response channels, and closes it again if that fails. ctx bounds
// dialing, authentication, selection and all subscription acknowledgements.
func (bc *broadcast) subscribe(ctx context.Context) (*redis.PubSubConn, error) {
	channels := []interface{}{bc.reqChannel, bc.resChannel}
	if bc.resources != nil {
		channels = bc.resources.channels()
	}
	sub, err := bc.subscribeChannels(ctx, channels)
	if err == nil {
		bc.markReady(channels)
	}
	return sub, err
}
func (bc *broadcast) subscribeChannels(ctx context.Context, channels []interface{}) (*redis.PubSubConn, error) {
	c, err := (*bc.dial.Load())(ctx)
	if err != nil {
		return nil, err
	}
	sub := &redis.PubSubConn{Conn: c}
	stop := context.AfterFunc(ctx, func() { _ = sub.Close() })
	defer stop()
	pending := map[string]bool{bc.pattern: true}
	for _, channel := range channels {
		pending[channel.(string)] = true
	}
	if err = sub.PSubscribe(bc.pattern); err == nil {
		err = sub.Subscribe(channels...)
	}
	for err == nil && len(pending) > 0 {
		switch m := sub.Receive().(type) {
		case redis.Subscription:
			if m.Kind == "subscribe" || m.Kind == "psubscribe" {
				delete(pending, m.Channel)
			}
		case error:
			err = m
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = sub.Close()
		return nil, err
	}
	if !stop() {
		_ = sub.Close()
		return nil, ctx.Err()
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
			if bc.resources != nil {
				bc.resources.dispatch(m)
			} else {
				bc.handleMessage(m)
			}

		case redis.Subscription:
			if m.Kind == "unsubscribe" {
				bc.acknowledgeUnsubscribe(sub, m.Channel)
			}
			bc.subscriptionChanged(m.Channel, m.Kind == "subscribe" || m.Kind == "psubscribe")
			if m.Count == 0 {
				return
			}
			// The server accepted the subscription, so the backoff starts over.
			delay = redisReconnectMin

		case error:
			bc.subscriptionChanged("", false)
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

		channels := []interface{}{bc.reqChannel, bc.resChannel}
		if bc.resources != nil {
			channels = bc.resources.channels()
		}
		ctx, cancel := context.WithTimeout(bc.transportCtx, bc.constructionTimeout())
		sub, err := bc.subscribeChannels(ctx, channels)
		cancel()
		if err != nil {
			continue
		}
		bc.subLock.Lock()
		select {
		case <-bc.done:
			bc.subLock.Unlock()
			_ = sub.Close()
			return nil, 0
		default:
		}
		if bc.resources != nil && !sameChannels(channels, bc.resources.channels()) {
			bc.subscriptionChanged("", false)
			bc.subLock.Unlock()
			_ = sub.Close()
			continue
		}
		bc.sub = sub
		bc.markReady(channels)
		bc.subLock.Unlock()
		return sub, min(2*delay, redisReconnectMax)
	}
}

// Close stops this adapter. Shared transports remain open for other adapters.
func (bc *broadcast) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), bc.constructionTimeout())
	defer cancel()
	return bc.closeContext(ctx)
}

func (bc *broadcast) closeContext(ctx context.Context) error {
	if bc.closed.Swap(true) {
		return nil
	}
	bc.notifyReadiness()
	bc.finishRequests()
	bc.BindSocketLookup(nil)
	if bc.resources != nil {
		return bc.resources.remove(ctx, bc)
	}
	bc.subLock.Lock()
	defer bc.subLock.Unlock()
	return bc.closeTransport()
}

// closeTransport requires subLock.
func (bc *broadcast) closeTransport() error {
	select {
	case <-bc.done:
		return nil
	default:
	}

	close(bc.done)
	if bc.transportCancel != nil {
		bc.transportCancel()
	}
	bc.subscriptionChanged("", false)
	_ = bc.sub.Close()
	_ = bc.pub.Close()
	return nil
}

func (bc *broadcast) handleMessage(m redis.Message) {
	if bc.closed.Load() {
		return
	}
	switch m.Channel {
	case bc.reqChannel:
		bc.onRequest(m.Data)
	case bc.resChannel:
		bc.onResponse(m.Data)
	default:
		_ = bc.onMessage(m.Channel, m.Data)
	}
}
