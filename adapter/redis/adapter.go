package redis

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// broadcast composes the local room index with Redis publication and requests.
type broadcast struct {
	// pub serves PUBLISH and PUBSUB commands. A redigo connection allows only
	// one concurrent caller, and these commands come from user goroutines and
	// the dispatch goroutine, so each command takes its own pooled connection.
	pub *redis.Pool
	// sub is read by the dispatch goroutine and replaced by it after a
	// receive error; subLock orders the replacement with close.
	sub     *redis.PubSubConn
	subLock sync.Mutex
	done    chan struct{}
	// dial opens a connection to the server (atomic so tests can wrap it
	// while the dispatcher runs); pattern is the broadcast channel pattern
	// the subscriber listens on.
	dial    atomic.Pointer[func(context.Context) (redis.Conn, error)]
	pattern string

	nsp        string
	uid        string
	key        string
	reqChannel string
	resChannel string

	requests    map[string]interface{}
	requestLock sync.Mutex

	*adapter.Memory
}

func newBroadcast(nsp string, opts *options) (*broadcast, error) {
	addr := opts.getAddr()
	var redisOpts []redis.DialOption
	if len(opts.Password) > 0 {
		redisOpts = append(redisOpts, redis.DialPassword(opts.Password))
	}
	if opts.DB > 0 {
		redisOpts = append(redisOpts, redis.DialDatabase(opts.DB))
	}

	// redis.DialContext and Pool.DialContext are missing from
	// redigo v2.0.0+incompatible, which gogf/gf v1 still selects, so ctx
	// reaches the dial through DialNetDial: it bounds the TCP dial, and once
	// ctx is done the connection is closed, which also ends AUTH and SELECT.
	dial := func(ctx context.Context) (redis.Conn, error) {
		stop := func() bool { return true }
		netDial := func(network, address string) (net.Conn, error) {
			// The timeouts redigo v1.8.9's own dialer uses when DialNetDial is not set.
			d := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 5 * time.Minute}
			c, err := d.DialContext(ctx, network, address)
			if err == nil {
				stop = context.AfterFunc(ctx, func() { _ = c.Close() })
			}
			return c, err
		}
		c, err := redis.Dial(opts.Network, addr, append(redisOpts[:len(redisOpts):len(redisOpts)], redis.DialNetDial(netDial))...)
		if !stop() && err == nil { // ctx ended as the dial finished: c is closed
			_ = c.Close()
			return nil, ctx.Err()
		}
		return c, err
	}
	pub := &redis.Pool{MaxIdle: 4, Dial: func() (redis.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), redisDialTimeout)
		defer cancel()
		return dial(ctx)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), redisDialTimeout)
	defer cancel()
	// Dial the first publishing connection now to report an unreachable server.
	first := pub.Get()
	err := first.Err()
	_ = first.Close()
	if err != nil {
		return nil, err
	}

	uid := uuid.NewString()
	rbc := &broadcast{
		Memory:     adapter.NewMemory(),
		requests:   make(map[string]interface{}),
		done:       make(chan struct{}),
		pub:        pub,
		pattern:    fmt.Sprintf("%s#%s#*", opts.Prefix, nsp),
		key:        fmt.Sprintf("%s#%s#%s", opts.Prefix, nsp, uid),
		reqChannel: fmt.Sprintf("%s-request#%s", opts.Prefix, nsp),
		resChannel: fmt.Sprintf("%s-response#%s", opts.Prefix, nsp),
		nsp:        nsp,
		uid:        uid,
	}
	rbc.dial.Store(&dial)

	if rbc.sub, err = rbc.subscribe(ctx); err != nil {
		_ = pub.Close()
		return nil, err
	}

	go rbc.dispatch()

	return rbc, nil
}

func (bc *broadcast) Clear(room string) { bc.Memory.Clear(room); go bc.publishClear(room) }

// Send emits to a room, registering a trailing callback on each local socket.
func (bc *broadcast) Send(room, event string, args ...interface{}) {
	_ = bc.SendTo(adapter.Options{Rooms: []string{room}}, event, args...)
}

// SendAll emits once per socket, registering a trailing callback on each local socket.
func (bc *broadcast) SendAll(event string, args ...interface{}) {
	_ = bc.SendTo(adapter.Options{}, event, args...)
}

// Rooms lists all cluster rooms for nil, or this socket's local rooms.
func (bc *broadcast) Rooms(connection adapter.Socket) []string {
	if connection == nil {
		return bc.AllRooms()
	}
	rooms := bc.Memory.Rooms(connection)
	if len(rooms) == 0 {
		return nil
	}
	return rooms
}

// SendTo excludes nothing on this adapter: Except returns adapter.ErrExceptUnsupported
// before local delivery or Redis I/O. Room unions dedupe locally and publish one
// message per room; remote sockets may receive once per room. Local suppresses
// publishing; Volatile and NoCompress are honoured locally and are not carried
// to other instances.
func (bc *broadcast) SendTo(opts adapter.Options, event string, args ...interface{}) error {
	if len(opts.Except) != 0 {
		return fmt.Errorf("redis broadcast: %w", adapter.ErrExceptUnsupported)
	}
	_ = bc.Memory.SendTo(opts, event, args...)
	if opts.Local {
		return nil
	}
	if len(opts.Rooms) == 0 {
		bc.publishMessage("", event, args...)
		return nil
	}
	seen := make(map[string]bool)
	for _, room := range opts.Rooms {
		if !seen[room] {
			seen[room] = true
			bc.publishRoomMessage(room, event, args...)
		}
	}
	return nil
}
