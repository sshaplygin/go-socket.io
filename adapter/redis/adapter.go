package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/logger"
)

// broadcast composes the local room index with Redis publication and requests.
type broadcast struct {
	clusterOnce    sync.Once
	requestTimeout time.Duration
	customHandler  func(context.Context, json.RawMessage) (json.RawMessage, error)
	clusterContext context.Context
	cancelCluster  context.CancelFunc
	remoteWorkers  chan struct{}
	lookupMu       sync.RWMutex
	socketLookup   func(string) adapter.Socket

	// pub serves PUBLISH and PUBSUB commands. A redigo connection allows only
	// one concurrent caller, and these commands come from user goroutines and
	// the dispatch goroutine, so each command takes its own pooled connection.
	transportCtx    context.Context
	transportCancel context.CancelFunc
	readiness       subscriptionState
	pub             *redis.Pool
	resources       *sharedResources
	closed          atomic.Bool // per-adapter state, independent from shared transport lifetime
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
	return newBroadcastWithResources(nsp, opts, nil)
}

func newBroadcastWithResources(nsp string, opts *options, shared *sharedResources) (*broadcast, error) {
	if nsp == "" {
		nsp = "/"
	}
	addr := opts.Addr
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
		pattern:    fmt.Sprintf("%s#*", opts.Prefix),
		key:        fmt.Sprintf("%s#%s#", opts.Prefix, nsp),
		reqChannel: fmt.Sprintf("%s-request#%s#", opts.Prefix, nsp),
		resChannel: fmt.Sprintf("%s-response#%s#", opts.Prefix, nsp),
		nsp:        nsp,
		uid:        uid,
	}
	rbc.transportCtx, rbc.transportCancel = context.WithCancel(context.Background())
	rbc.initCluster(opts)
	rbc.dial.Store(&dial)
	if shared != nil {
		rbc.resources = shared
		rbc.pattern = fmt.Sprintf("%s#*", opts.Prefix)
		shared.owner = rbc
		shared.members[nsp] = []*broadcast{rbc}
	}

	if rbc.sub, err = rbc.subscribe(ctx); err != nil {
		_ = pub.Close()
		rbc.transportCancel()
		return nil, err
	}

	go rbc.dispatch()

	return rbc, nil
}

func (bc *broadcast) Clear(room string) {
	bc.logRequestError(bc.ClearContext(context.Background(), room))
}

// Send emits to a room. A trailing callback rejects the entire broadcast.
// Rejection and publication failures are logged at WARN.
func (bc *broadcast) Send(room, event string, args ...interface{}) {
	if err := bc.SendTo(adapter.Options{Rooms: []string{room}}, event, args...); err != nil {
		logger.Log.Warn("socketio: redis broadcast failed", "nsp", bc.nsp, "err", err)
	}
}

// SendAll emits once per socket. A trailing callback rejects the entire broadcast.
// Rejection and publication failures are logged at WARN.
func (bc *broadcast) SendAll(event string, args ...interface{}) {
	if err := bc.SendTo(adapter.Options{}, event, args...); err != nil {
		logger.Log.Warn("socketio: redis broadcast failed", "nsp", bc.nsp, "err", err)
	}
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

// SendTo publishes one union with exclusion and packet flags. Encoding errors
// precede delivery; Redis errors may follow local delivery, which is not rolled back.
func (bc *broadcast) SendTo(opts adapter.Options, event string, args ...interface{}) error {
	if bc.closed.Load() {
		return errAdapterClosed
	}
	if opts.Local {
		return bc.Memory.SendTo(opts, event, args...)
	}
	data, err := encodeBroadcast(bc.uid, bc.nsp, event, opts, args)
	if err != nil {
		return err
	}
	if err = bc.Memory.SendTo(opts, event, args...); err != nil {
		return err
	}
	channel := bc.broadcastChannel()
	if len(opts.Rooms) == 1 {
		channel += opts.Rooms[0] + "#"
	}
	_, err = bc.do("PUBLISH", channel, data)
	return err
}
