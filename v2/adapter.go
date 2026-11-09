package socketio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/sshaplygin/go-socket.io/v2/parser"
)

// Adapter keeps room membership of one namespace and delivers broadcasts. The
// in-memory implementation is in this package; external adapters import this package
// and the root never imports them.
//
// Room mutations and SocketRooms are local; Sockets may query the cluster. An empty
// room filter selects all. Broadcast unions rooms, deduplicates recipients, applies
// exclusions and returns partial progress together with any error; Published means
// broker acceptance, never remote delivery or acknowledgement; a Local broadcast never
// publishes. Sockets and FetchSockets may return deduplicated partial data with a
// deadline error. ServerSideEmit success means publication was accepted. Implementations
// copy borrowed input they retain and return owned snapshots. Close releases adapter
// workers and subscriptions, never an application-owned broker client.
type Adapter interface {
	AddAll(sid SocketID, rooms []Room)
	Del(sid SocketID, room Room)
	DelAll(sid SocketID)
	Broadcast(ctx context.Context, pkt parser.Packet, opts BroadcastOptions) (BroadcastResult, error)
	Sockets(ctx context.Context, rooms []Room) ([]SocketID, error)
	SocketRooms(sid SocketID) []Room
	FetchSockets(ctx context.Context, opts BroadcastOptions) ([]RemoteSocket, error)
	ServerSideEmit(ctx context.Context, event string, args ...any) error
	Close() error
}

// AdapterFactory builds the adapter of nsp. The server calls it from Server.Namespace
// with its own context, which is cancelled when Shutdown or Close begins. That context
// bounds only the factory call: an adapter derives no lifetime from it and lives until
// Adapter.Close.
type AdapterFactory func(ctx context.Context, nsp *Namespace) (Adapter, error)

// LocalSockets is the seam through which an adapter reaches the sockets of its own
// server. A namespace hands it out through Namespace.LocalSockets; an adapter that
// receives a broadcast from a peer selects local recipients from its own room
// membership and calls Deliver for each. The memory adapter takes it as a constructor
// argument, so its conformance tests run against a test double before the runtime exists.
type LocalSockets interface {
	// Deliver queues pkt on the local socket sid and returns nil only when it was
	// enqueued; BroadcastResult.LocalRecipients counts those successes. A socket that
	// is gone yields an error matching ErrSocketClosed, a full queue ErrWriteBufferFull.
	Deliver(ctx context.Context, sid SocketID, pkt parser.Packet) error
	// Snapshot returns the RemoteSocket of the local socket sid, with the Handshake
	// already passed through RedactHandshake as the RemoteSocket godoc requires; false
	// when the socket is gone.
	Snapshot(sid SocketID) (RemoteSocket, bool)
}

// BroadcastOptions selects the recipients of Adapter.Broadcast and FetchSockets.
type BroadcastOptions struct {
	Rooms, Except []Room
	Flags         BroadcastFlags
}

// BroadcastFlags selects how far a broadcast or query reaches. Local restricts the
// operation to the server's own sockets; a local operation never publishes. The
// other flags of the Node adapter (volatile, compress, timeout) are not part of this
// contract: a later stage may add them as further fields, so construct the struct
// with field names.
type BroadcastFlags struct{ Local bool }

// RemoteSocket is an owned metadata snapshot of a socket, possibly on another node,
// shaped like an entry of Node's fetchSockets: ID, Rooms, Handshake and Data.
//
// Handshake is a JSON object with the Node key names (headers, time, address, xdomain,
// secure, issued, url, query). The guarantee is exactly this: no auth key, and no
// authorization, cookie or proxy-authorization entry in headers (names compared
// without case); the producer drops them, which for a snapshot decoded from a peer is
// the adapter that decoded it, by calling RedactHandshake. Nothing else is redacted: url, query, address and every
// other header may carry credentials and are passed through as Node does. Data is the slot of Node's socket.data: nil
// means none, otherwise valid JSON; binary values are not representable. A producer
// that cannot encode Data returns the entries it can and an error. How an application
// sets Data is defined by stage 2.3S. Producers copy every slice, and mutating a snapshot must not change
// adapter state.
type RemoteSocket struct {
	ID        SocketID
	Rooms     []Room
	Handshake json.RawMessage
	Data      json.RawMessage
}

// RedactHandshake returns a copy of the Handshake object h without the auth key and
// without the authorization, cookie and proxy-authorization entries of headers (names
// compared without case); every other key and header is kept byte for byte. It is the
// one implementation of the RemoteSocket guarantee: the server uses it for local
// snapshots and an adapter calls it on a Handshake decoded from a peer, because a Node
// peer sends the omitted values. nil yields nil; h that is not a JSON object yields an
// error. Kept members are written in source order without whitespace between them, and
// the result never aliases h. It lives in the root package, which adapters already
// import and which the graph allows to call it without an import of adapter/....
func RedactHandshake(h json.RawMessage) (json.RawMessage, error) { return redactHandshake(h) }

// memoryAdapter is the in-memory Adapter of a single server, after socket.io-adapter's
// in-memory Adapter. It keeps the membership in both directions and reaches sockets only
// through LocalSockets. Every lock is released before LocalSockets is called, so a
// recipient that blocks in Deliver, or a Snapshot that reads the adapter, delays no
// membership change.
type memoryAdapter struct {
	local LocalSockets

	mu    sync.RWMutex
	rooms map[Room]map[SocketID]struct{}
	sids  map[SocketID]map[Room]struct{}
}

var _ Adapter = (*memoryAdapter)(nil)

// newMemoryAdapter returns an empty in-memory adapter that delivers through local.
func newMemoryAdapter(local LocalSockets) *memoryAdapter {
	return &memoryAdapter{
		local: local,
		rooms: make(map[Room]map[SocketID]struct{}),
		sids:  make(map[SocketID]map[Room]struct{}),
	}
}

// memoryAdapterFactory is the default AdapterFactory: an in-memory adapter over
// nsp.LocalSockets(). The adapter derives nothing from ctx.
func memoryAdapterFactory(_ context.Context, nsp *Namespace) (Adapter, error) {
	local := nsp.LocalSockets()
	if local == nil {
		return nil, fmt.Errorf("socketio: memory adapter: namespace %q has no local sockets", nsp.Name())
	}
	return newMemoryAdapter(local), nil
}

// AddAll adds sid to rooms. Like Node, it records sid even when rooms is empty.
func (a *memoryAdapter) AddAll(sid SocketID, rooms []Room) {
	a.mu.Lock()
	defer a.mu.Unlock()
	joined := a.sids[sid]
	if joined == nil {
		joined = make(map[Room]struct{}, len(rooms))
		a.sids[sid] = joined
	}
	for _, room := range rooms {
		joined[room] = struct{}{}
		members := a.rooms[room]
		if members == nil {
			members = make(map[SocketID]struct{})
			a.rooms[room] = members
		}
		members[sid] = struct{}{}
	}
}

// Del removes sid from room and drops the room once it is empty. Like Node, sid stays
// recorded with no rooms after it left its last one.
func (a *memoryAdapter) Del(sid SocketID, room Room) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sids[sid], room)
	a.leaveLocked(sid, room)
}

// DelAll removes sid from every room and forgets it.
func (a *memoryAdapter) DelAll(sid SocketID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for room := range a.sids[sid] {
		a.leaveLocked(sid, room)
	}
	delete(a.sids, sid)
}

func (a *memoryAdapter) leaveLocked(sid SocketID, room Room) {
	members, ok := a.rooms[room]
	if !ok {
		return
	}
	delete(members, sid)
	if len(members) == 0 {
		delete(a.rooms, room)
	}
}

// selectLocked returns the IDs in any of rooms, or every recorded ID when rooms is
// empty, without the members of any except room; each ID once. a.mu must be held.
func (a *memoryAdapter) selectLocked(rooms, except []Room) []SocketID {
	var excluded map[SocketID]struct{}
	for _, room := range except {
		for sid := range a.rooms[room] {
			if excluded == nil {
				excluded = make(map[SocketID]struct{})
			}
			excluded[sid] = struct{}{}
		}
	}
	if len(rooms) == 0 {
		ids := make([]SocketID, 0, len(a.sids))
		for sid := range a.sids {
			if _, skip := excluded[sid]; !skip {
				ids = append(ids, sid)
			}
		}
		return ids
	}
	var ids []SocketID
	seen := make(map[SocketID]struct{})
	for _, room := range rooms {
		for sid := range a.rooms[room] {
			_, skip := excluded[sid]
			if _, dup := seen[sid]; skip || dup {
				continue
			}
			seen[sid] = struct{}{}
			ids = append(ids, sid)
		}
	}
	return ids
}

// Broadcast delivers pkt to the selected sockets one after another and never
// publishes. A failed delivery is reported and not retried, and the remaining
// recipients are still tried; once ctx is done no further delivery starts.
// LocalRecipients counts the deliveries that succeeded. The error reports the
// failures grouped by cause (see deliveryFailures), so its size does not grow with
// the number of recipients. It matches with errors.Is ErrSocketClosed,
// ErrWriteBufferFull, ctx.Err() when ctx ended the broadcast, the first
// maxFailureCauses other causes and the first of the causes after them; those later
// causes are otherwise only counted.
func (a *memoryAdapter) Broadcast(ctx context.Context, pkt parser.Packet, opts BroadcastOptions) (BroadcastResult, error) {
	a.mu.RLock()
	ids := a.selectLocked(opts.Rooms, opts.Except)
	a.mu.RUnlock()
	var res BroadcastResult
	var failures deliveryFailures
	var stopped error
	for i, sid := range ids {
		if err := ctx.Err(); err != nil {
			stopped = fmt.Errorf("socketio: broadcast stopped with %d of %d recipients not tried: %w", len(ids)-i, len(ids), err)
			break
		}
		if err := a.local.Deliver(ctx, sid, pkt); err != nil {
			failures.add(sid, err)
			continue
		}
		res.LocalRecipients++
	}
	return res, errors.Join(append(failures.errs(len(ids)), stopped)...)
}

const (
	// maxFailureCauses bounds the causes other than the two sentinels that a broadcast
	// error reports one by one; failures of further causes are counted together under
	// the first of them.
	maxFailureCauses = 8
	// maxFailureSamples bounds the socket IDs a broadcast error names per cause.
	maxFailureSamples = 3
)

// deliveryFailures groups the failed deliveries of one broadcast by cause: the two
// failures of the LocalSockets contract (ErrSocketClosed, ErrWriteBufferFull) by their
// sentinel, always in a group of their own, any other error by its message. A group
// keeps its first error, which the reported error wraps, a count and a few socket IDs.
type deliveryFailures struct {
	groups   []failureGroup
	others   int // groups of causes other than the sentinels
	overflow *failureGroup
}

type failureGroup struct {
	cause   string
	first   error
	count   int
	samples []SocketID
}

func (f *deliveryFailures) add(sid SocketID, err error) {
	cause, sentinel := err.Error(), true
	switch {
	case errors.Is(err, ErrSocketClosed):
		cause = ErrSocketClosed.Error()
	case errors.Is(err, ErrWriteBufferFull):
		cause = ErrWriteBufferFull.Error()
	default:
		sentinel = false
	}
	var g *failureGroup
	for i := range f.groups {
		if f.groups[i].cause == cause {
			g = &f.groups[i]
			break
		}
	}
	switch {
	case g != nil:
	case sentinel || f.others < maxFailureCauses:
		if !sentinel {
			f.others++
		}
		f.groups = append(f.groups, failureGroup{cause: cause, first: err})
		g = &f.groups[len(f.groups)-1]
	default:
		if f.overflow == nil {
			f.overflow = &failureGroup{first: err}
		}
		g = f.overflow
	}
	g.count++
	if len(g.samples) < maxFailureSamples {
		g.samples = append(g.samples, sid)
	}
}

// errs returns one error per group, out of total selected recipients.
func (f *deliveryFailures) errs(total int) []error {
	out := make([]error, 0, len(f.groups)+1)
	for _, g := range f.groups {
		out = append(out, fmt.Errorf("socketio: broadcast: %d of %d recipients failed (for example %q): %w", g.count, total, g.samples, g.first))
	}
	if g := f.overflow; g != nil {
		out = append(out, fmt.Errorf("socketio: broadcast: %d of %d recipients failed with further causes (for example %q): %w", g.count, total, g.samples, g.first))
	}
	return out
}

// Sockets returns the IDs of the selected sockets that are still present, as Node's
// sockets lists only sockets of the namespace; Snapshot is the presence check of the
// seam. A single server expects no peer, so it returns at once with a nil error.
func (a *memoryAdapter) Sockets(_ context.Context, rooms []Room) ([]SocketID, error) {
	a.mu.RLock()
	ids := a.selectLocked(rooms, nil)
	a.mu.RUnlock()
	present := ids[:0]
	for _, sid := range ids {
		if _, ok := a.local.Snapshot(sid); ok {
			present = append(present, sid)
		}
	}
	return present, nil
}

// SocketRooms returns a copy of the rooms of sid: nil when sid is not recorded, an
// empty slice when it is recorded in no room.
func (a *memoryAdapter) SocketRooms(sid SocketID) []Room {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.roomsLocked(sid)
}

func (a *memoryAdapter) roomsLocked(sid SocketID) []Room {
	joined, ok := a.sids[sid]
	if !ok {
		return nil
	}
	rooms := make([]Room, 0, len(joined))
	for room := range joined {
		rooms = append(rooms, room)
	}
	return rooms
}

// FetchSockets returns the Snapshot of each selected socket that is still present.
// Rooms is the adapter's membership at selection time, as a Node socket reports the
// rooms of its adapter; Handshake and Data are the Snapshot's, which the seam returns
// owned and redacted. A single server expects no peer, so it returns at once with a
// nil error.
func (a *memoryAdapter) FetchSockets(_ context.Context, opts BroadcastOptions) ([]RemoteSocket, error) {
	a.mu.RLock()
	ids := a.selectLocked(opts.Rooms, opts.Except)
	rooms := make([][]Room, len(ids))
	for i, sid := range ids {
		rooms[i] = a.roomsLocked(sid)
	}
	a.mu.RUnlock()
	out := make([]RemoteSocket, 0, len(ids))
	for i, sid := range ids {
		rs, ok := a.local.Snapshot(sid)
		if !ok {
			continue
		}
		rs.Rooms = rooms[i]
		out = append(out, rs)
	}
	return out, nil
}

// ServerSideEmit has no peer to publish to on a single server and returns nil, as
// Node's in-memory adapter neither delivers nor fails a server-side emit.
func (*memoryAdapter) ServerSideEmit(context.Context, string, ...any) error { return nil }

// Close returns nil: the adapter owns no worker or subscription. As in Node, the
// adapter keeps working after Close, and closing it again is harmless.
func (*memoryAdapter) Close() error { return nil }
