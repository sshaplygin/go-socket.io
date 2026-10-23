package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/logger"
)

// ErrRequestTimeout means not all counted subscribers answered within the bound.
var ErrRequestTimeout = errors.New("redis: cluster request timeout")

// ErrAdapterClosed means this namespace adapter has been closed.
var ErrAdapterClosed = errAdapterClosed

// Cluster is the optional standalone Redis cluster management API. Obtain it by
// asserting the adapter returned by a Redis factory or ServerNamespace.Adapter.
// Queries return validated partial snapshots with timeout errors, never an atomic
// cluster snapshot. All calls use the earlier of ctx and WithRequestsTimeout.
type Cluster interface {
	adapter.Adapter
	// IDsContext returns the namespace's socket IDs in the room union, or all IDs.
	// A timeout returns validated partial results with ErrRequestTimeout.
	IDsContext(ctx context.Context, rooms ...string) ([]string, error)
	// AllRoomsContext returns cluster rooms; timeouts preserve validated partial results.
	AllRoomsContext(ctx context.Context) ([]string, error)
	// LenContext counts room members; a timeout returns an error instead of a count.
	LenContext(ctx context.Context, room string) (int, error)
	// ClearContext removes a snapshot of members under one deadline without rollback.
	ClearContext(ctx context.Context, room string) error
	// ClientRooms queries the rooms of sid and returns timeout or cancellation errors.
	ClientRooms(ctx context.Context, sid string) ([]string, error)
	// RemoteJoin joins sid to room; cancellation does not roll back completed changes.
	RemoteJoin(ctx context.Context, sid, room string) error
	// RemoteLeave removes sid from room; cancellation does not roll back completed changes.
	RemoteLeave(ctx context.Context, sid, room string) error
	// RemoteDisconnect disconnects sid, closing its engine when closeEngine is true.
	RemoteDisconnect(ctx context.Context, sid string, closeEngine bool) error
	// CustomRequest collects JSON replies; timeouts return validated partial results.
	CustomRequest(ctx context.Context, data json.RawMessage) ([]json.RawMessage, error)
}

func (bc *broadcast) initCluster(opts *options) {
	bc.clusterOnce.Do(func() {
		bc.requestTimeout = redisRequestTimeout
		bc.customHandler = func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage("null"), nil }
		if opts != nil {
			if opts.RequestsTimeout > 0 {
				bc.requestTimeout = opts.RequestsTimeout
			}
			if opts.CustomRequestHandler != nil {
				bc.customHandler = opts.CustomRequestHandler
			}
		}
		bc.clusterContext, bc.cancelCluster = context.WithCancel(context.Background())
		bc.remoteWorkers = make(chan struct{}, 32)
	})
}
func (bc *broadcast) finishRequests() {
	bc.initCluster(nil)
	bc.cancelCluster()
	bc.requestLock.Lock()
	for id, value := range bc.requests {
		if request, ok := value.(*clusterRequest); ok {
			request.finish(ErrAdapterClosed)
		}
		delete(bc.requests, id)
	}
	bc.requestLock.Unlock()
}

// BindSocketLookup binds the owning server's independent connected-socket index.
// Passing nil clears the binding when its namespace adapter is retired.
func (bc *broadcast) BindSocketLookup(lookup func(string) adapter.Socket) {
	bc.lookupMu.Lock()
	bc.socketLookup = lookup
	bc.lookupMu.Unlock()
}
func (bc *broadcast) lookupSocket(id string) adapter.Socket {
	bc.lookupMu.RLock()
	lookup := bc.socketLookup
	bc.lookupMu.RUnlock()
	if lookup != nil {
		return lookup(id)
	}
	// Socket IDs need not remain a room after LeaveAll. Standalone adapter users
	// without a binding have only the current memory index to search.
	var socket adapter.Socket
	for _, room := range bc.Memory.AllRooms() {
		bc.Memory.ForEach(room, func(candidate adapter.Socket) {
			if socket == nil && candidate.ID() == id {
				socket = candidate
			}
		})
		if socket != nil {
			break
		}
	}
	return socket
}
func (bc *broadcast) runClusterWork(work func(context.Context)) {
	bc.initCluster(nil)
	semaphore := bc.remoteWorkers
	if bc.resources != nil {
		semaphore = bc.resources.clusterWorkers()
	}
	select {
	case semaphore <- struct{}{}:
	default:
		logger.Log.Warn("socketio: redis remote request worker limit reached", "nsp", bc.nsp)
		return
	}
	go func() {
		defer func() { <-semaphore }()
		ctx, cancel := context.WithTimeout(bc.clusterContext, bc.requestTimeout)
		defer cancel()
		if ctx.Err() == nil {
			work(ctx)
		}
	}()
}

// boundedDo uses a dedicated connection so cancellation can close the actual
// transport safely without returning an in-flight pooled connection to another caller.
func (bc *broadcast) boundedDo(ctx context.Context, cmd string, args ...interface{}) (interface{}, error) {
	bc.initCluster(nil)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bc.closed.Load() {
		return nil, ErrAdapterClosed
	}
	dialContext, cancel := context.WithCancel(ctx)
	stopLife := context.AfterFunc(bc.clusterContext, cancel)
	defer func() { stopLife(); cancel() }()
	dial := bc.transport().dial.Load()
	if dial == nil {
		return nil, fmt.Errorf("redis: missing command dialer")
	}
	conn, err := (*dial)(dialContext)
	if err != nil {
		if bc.closed.Load() {
			return nil, ErrAdapterClosed
		}
		if cause := requestDeadlineError(ctx); cause != nil {
			return nil, cause
		}
		return nil, err
	}
	stop := context.AfterFunc(dialContext, func() { _ = conn.Close() })
	defer func() { stop(); _ = conn.Close() }()
	reply, err := conn.Do(cmd, args...)
	if bc.closed.Load() {
		return nil, ErrAdapterClosed
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return reply, err
}

func parseSubscriberCount(reply interface{}, channel string) (int, error) {
	values, err := redigo.Values(reply, nil)
	if err != nil || len(values) != 2 {
		return 0, fmt.Errorf("redis: malformed NUMSUB response")
	}
	name, err := redigo.String(values[0], nil)
	if err != nil || name != channel {
		return 0, fmt.Errorf("redis: malformed NUMSUB channel")
	}
	count, err := redigo.Int(values[1], nil)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("redis: malformed NUMSUB count")
	}
	return count, nil
}

var _ Cluster = (*broadcast)(nil)
