package socketio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/v2/parser"
)

// fakeLocal is the test double of the namespace side of LocalSockets. A present socket
// has a raw handshake as a Node peer would send it; Snapshot redacts it, as the seam
// requires of its producer. Deliver records every attempt, fails with the error set
// in fail, then with ErrSocketClosed for a socket that is not present.
type fakeLocal struct {
	mu        sync.Mutex
	live      map[SocketID]json.RawMessage
	fail      map[SocketID]error
	attempts  []SocketID
	delivered map[SocketID]parser.Packet
	onDeliver func(SocketID) // called before the outcome is decided, without the lock
}

func newFakeLocal(ids ...SocketID) *fakeLocal {
	f := &fakeLocal{live: map[SocketID]json.RawMessage{}, fail: map[SocketID]error{}, delivered: map[SocketID]parser.Packet{}}
	for _, id := range ids {
		f.live[id] = json.RawMessage(`{}`)
	}
	return f
}

func (f *fakeLocal) Deliver(_ context.Context, sid SocketID, pkt parser.Packet) error {
	f.mu.Lock()
	f.attempts = append(f.attempts, sid)
	hook := f.onDeliver
	f.mu.Unlock()
	if hook != nil {
		hook(sid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[sid]; err != nil {
		return err
	}
	if _, ok := f.live[sid]; !ok {
		return ErrSocketClosed
	}
	f.delivered[sid] = pkt
	return nil
}

func (f *fakeLocal) Snapshot(sid SocketID) (RemoteSocket, bool) {
	f.mu.Lock()
	raw, ok := f.live[sid]
	f.mu.Unlock()
	if !ok {
		return RemoteSocket{}, false
	}
	h, err := RedactHandshake(raw)
	if err != nil {
		panic(err)
	}
	return RemoteSocket{ID: sid, Handshake: h}, true
}

func (f *fakeLocal) setLive(sid SocketID, handshake string, present bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if present {
		f.live[sid] = json.RawMessage(handshake)
	} else {
		delete(f.live, sid)
	}
}

func (f *fakeLocal) deliveredIDs() []SocketID {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]SocketID, 0, len(f.delivered))
	for id := range f.delivered {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (f *fakeLocal) attempted() []SocketID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.attempts)
}

func sorted[T ~string](s []T) []T {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

var testPacket = parser.Packet{Type: parser.Event, Namespace: "/rooms", Data: json.RawMessage(`["notice",{"value":1}]`)}

func TestMemoryAdapterPartialFailure(t *testing.T) {
	local := newFakeLocal("a", "b", "c", "d")
	local.fail["b"] = fmt.Errorf("queue of b: %w", ErrWriteBufferFull)
	a := newMemoryAdapter(local)
	for _, id := range []SocketID{"a", "b", "c", "d", "gone"} {
		a.AddAll(id, []Room{id, "all"})
	}
	res, err := a.Broadcast(context.Background(), testPacket, BroadcastOptions{Rooms: []Room{"all"}, Except: []Room{"d"}})
	if res.LocalRecipients != 2 || res.Published {
		t.Fatalf("result = %+v, want 2 local recipients, not published", res)
	}
	if !errors.Is(err, ErrWriteBufferFull) || !errors.Is(err, ErrSocketClosed) {
		t.Fatalf("error %v does not match ErrWriteBufferFull and ErrSocketClosed", err)
	}
	if got := local.deliveredIDs(); !slices.Equal(got, []SocketID{"a", "c"}) {
		t.Errorf("delivered to %v, want [a c]", got)
	}
	// Every selected socket is tried exactly once: a failure is neither retried nor
	// does it stop the remaining deliveries.
	if got := sorted(local.attempted()); !slices.Equal(got, []SocketID{"a", "b", "c", "gone"}) {
		t.Errorf("attempts %v, want each of a, b, c, gone once", got)
	}
}

// TestMemoryAdapterBroadcastErrorIsBounded fails most of 1000 deliveries, with the two
// sentinels and 250 distinct other errors, and cancels the context on the way: the
// error stays small and still matches each sentinel and the context error.
func TestMemoryAdapterBroadcastErrorIsBounded(t *testing.T) {
	const n = 1000
	local := newFakeLocal()
	a := newMemoryAdapter(local)
	for i := 0; i < n; i++ {
		sid := SocketID(fmt.Sprintf("s%04d", i))
		local.setLive(sid, `{}`, true)
		a.AddAll(sid, []Room{sid})
		switch i % 4 {
		case 0:
			local.fail[sid] = fmt.Errorf("socket %s gone: %w", sid, ErrSocketClosed)
		case 1:
			local.fail[sid] = fmt.Errorf("queue of %s: %w", sid, ErrWriteBufferFull)
		case 2:
			local.fail[sid] = fmt.Errorf("transport of %s failed", sid)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	local.onDeliver = func(SocketID) {
		if attempts++; attempts == n-100 {
			cancel()
		}
	}
	res, err := a.Broadcast(ctx, testPacket, BroadcastOptions{})
	if res.LocalRecipients != len(local.deliveredIDs()) || attempts != n-100 {
		t.Fatalf("result %+v after %d attempts, %d delivered", res, attempts, len(local.deliveredIDs()))
	}
	for _, target := range []error{ErrSocketClosed, ErrWriteBufferFull, context.Canceled} {
		if !errors.Is(err, target) {
			t.Errorf("error does not match %v", target)
		}
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) > 12 || len(err.Error()) > 4096 {
		t.Errorf("error is not bounded: %d bytes\n%v", len(err.Error()), err)
	}
}

// TestMemoryAdapterBroadcastErrorKeepsSentinels fails more distinct other causes than
// the error reports one by one, then ErrSocketClosed twice and ErrWriteBufferFull once:
// both sentinels stay matchable with errors.Is.
func TestMemoryAdapterBroadcastErrorKeepsSentinels(t *testing.T) {
	const others = maxFailureCauses + 4
	const n = others + 4 // the last delivery succeeds
	local := newFakeLocal()
	a := newMemoryAdapter(local)
	for i := 0; i < n; i++ {
		sid := SocketID(fmt.Sprintf("s%02d", i))
		local.setLive(sid, `{}`, true)
		a.AddAll(sid, []Room{sid})
	}
	// The outcome follows the attempt number, not the socket, so the order in which
	// the adapter selects sockets does not matter.
	attempt := 0
	local.onDeliver = func(sid SocketID) {
		attempt++
		err := fmt.Errorf("cause %d", attempt)
		switch {
		case attempt > others+3:
			return
		case attempt > others+2:
			err = ErrWriteBufferFull
		case attempt > others:
			err = ErrSocketClosed
		}
		local.mu.Lock()
		local.fail[sid] = err
		local.mu.Unlock()
	}
	res, err := a.Broadcast(context.Background(), testPacket, BroadcastOptions{})
	if res.LocalRecipients != 1 || !errors.Is(err, ErrSocketClosed) || !errors.Is(err, ErrWriteBufferFull) {
		t.Fatalf("result %+v, error %v; want 1 recipient and both sentinels matched", res, err)
	}
	// Each part reports its count; the causes past the cap share one counted part.
	parts, counts := map[string]int{}, map[string]int{}
	for _, part := range err.(interface{ Unwrap() []error }).Unwrap() {
		var c int
		if _, scanErr := fmt.Sscanf(part.Error(), "socketio: broadcast: %d of", &c); scanErr != nil {
			t.Fatalf("part %q has no count: %v", part, scanErr)
		}
		kind := "other"
		switch {
		case errors.Is(part, ErrSocketClosed):
			kind = "closed"
		case errors.Is(part, ErrWriteBufferFull):
			kind = "full"
		case strings.Contains(part.Error(), "further causes"):
			kind = "further"
		}
		parts[kind]++
		counts[kind] += c
	}
	wantParts := map[string]int{"other": maxFailureCauses, "further": 1, "closed": 1, "full": 1}
	wantCounts := map[string]int{"other": maxFailureCauses, "further": others - maxFailureCauses, "closed": 2, "full": 1}
	if !maps.Equal(parts, wantParts) || !maps.Equal(counts, wantCounts) {
		t.Errorf("parts %v with counts %v, want %v with %v:\n%v", parts, counts, wantParts, wantCounts, err)
	}
}

func TestMemoryAdapterBroadcastStopsWhenContextEnds(t *testing.T) {
	local := newFakeLocal("a", "b", "c")
	a := newMemoryAdapter(local)
	for _, id := range []SocketID{"a", "b", "c"} {
		a.AddAll(id, []Room{id})
	}
	ctx, cancel := context.WithCancel(context.Background())
	local.onDeliver = func(SocketID) { cancel() }
	res, err := a.Broadcast(ctx, testPacket, BroadcastOptions{})
	if res.LocalRecipients != 1 || len(local.attempted()) != 1 {
		t.Fatalf("result %+v after %d attempts, want the delivery in progress only", res, len(local.attempted()))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v does not match context.Canceled", err)
	}

	res, err = a.Broadcast(ctx, testPacket, BroadcastOptions{})
	if res.LocalRecipients != 0 || len(local.attempted()) != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("done context: result %+v, error %v, attempts %d; want nothing tried", res, err, len(local.attempted()))
	}
}

// TestMemoryAdapterRedactedSnapshot is the redaction case of ROADMAP 2.2 for the memory
// adapter: a snapshot that carries auth, the three headers in mixed case, x-api-key and
// a query token reaches FetchSockets without the first two groups and with the other
// two unchanged.
func TestMemoryAdapterRedactedSnapshot(t *testing.T) {
	const raw = `{"headers":{"AUTHORIZATION":"Bearer s1","Cookie":"sid=s2","proxy-Authorization":"Basic s3","x-api-key":"k-1","host":"h"},"url":"/socket.io/?EIO=4&token=q-1","query":{"EIO":"4","token":"q-1"},"auth":{"token":"s4"}}`
	local := newFakeLocal()
	local.setLive("a", raw, true)
	a := newMemoryAdapter(local)
	a.AddAll("a", []Room{"a", "red"})
	got, err := a.FetchSockets(context.Background(), BroadcastOptions{Rooms: []Room{"red"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("FetchSockets = %v, %v; want one socket", got, err)
	}
	if got[0].ID != "a" || !slices.Equal(sorted(got[0].Rooms), []Room{"a", "red"}) {
		t.Errorf("snapshot %s in %v, want a in [a red]", got[0].ID, got[0].Rooms)
	}
	var hs map[string]json.RawMessage
	if err := json.Unmarshal(got[0].Handshake, &hs); err != nil {
		t.Fatal(err)
	}
	var headers map[string]json.RawMessage
	if err := json.Unmarshal(hs["headers"], &headers); err != nil {
		t.Fatal(err)
	}
	if _, ok := hs["auth"]; ok {
		t.Errorf("auth reached FetchSockets: %s", got[0].Handshake)
	}
	for _, s := range []string{"s1", "s2", "s3", "s4"} {
		if bytes.Contains(got[0].Handshake, []byte(s)) {
			t.Errorf("secret %s reached FetchSockets: %s", s, got[0].Handshake)
		}
	}
	if string(headers["x-api-key"]) != `"k-1"` || string(hs["query"]) != `{"EIO":"4","token":"q-1"}` ||
		string(hs["url"]) != `"/socket.io/?EIO=4&token=q-1"` || len(headers) != 2 {
		t.Errorf("passed-through fields changed: %s", got[0].Handshake)
	}
}

// TestMemoryAdapterQueriesIgnoreContext pins ROADMAP 2.2 Queries: with no peer expected,
// Sockets and FetchSockets return the local data and a nil error at once, also for a
// ctx that is already done.
func TestMemoryAdapterQueriesIgnoreContext(t *testing.T) {
	local := newFakeLocal("a", "b")
	a := newMemoryAdapter(local)
	a.AddAll("a", []Room{"a", "red"})
	a.AddAll("b", []Room{"b"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	ids, idsErr := a.Sockets(ctx, nil)
	snaps, snapsErr := a.FetchSockets(ctx, BroadcastOptions{})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("queries took %v with a done context, want at once", elapsed)
	}
	if idsErr != nil || !slices.Equal(sorted(ids), []SocketID{"a", "b"}) {
		t.Errorf("Sockets = %v, %v; want [a b] and nil", ids, idsErr)
	}
	if snapsErr != nil || len(snaps) != 2 {
		t.Errorf("FetchSockets = %d sockets, %v; want 2 and nil", len(snaps), snapsErr)
	}
}

func TestMemoryAdapterOwnedCopies(t *testing.T) {
	local := newFakeLocal("a")
	a := newMemoryAdapter(local)
	in := []Room{"a", "red"}
	a.AddAll("a", in)
	in[1] = "changed"

	rooms := a.SocketRooms("a")
	rooms[0], rooms[1] = "x", "y"
	ids, _ := a.Sockets(context.Background(), nil)
	ids[0] = "x"
	snaps, _ := a.FetchSockets(context.Background(), BroadcastOptions{})
	snaps[0].Rooms[0] = "x"

	if got := sorted(a.SocketRooms("a")); !slices.Equal(got, []Room{"a", "red"}) {
		t.Errorf("SocketRooms = %v after mutating results and input, want [a red]", got)
	}
	if got, _ := a.Sockets(context.Background(), []Room{"red"}); !slices.Equal(got, []SocketID{"a"}) {
		t.Errorf("Sockets(red) = %v, want [a]", got)
	}
}

// TestMemoryAdapterDeliveryHoldsNoLock blocks a recipient inside Deliver and changes
// and reads membership meanwhile: no adapter lock may be held across Deliver.
func TestMemoryAdapterDeliveryHoldsNoLock(t *testing.T) {
	local := newFakeLocal("a", "b")
	a := newMemoryAdapter(local)
	a.AddAll("a", []Room{"a"})
	entered, release := make(chan struct{}), make(chan struct{})
	local.onDeliver = func(SocketID) {
		close(entered)
		<-release
	}
	done := make(chan error)
	go func() {
		_, err := a.Broadcast(context.Background(), testPacket, BroadcastOptions{})
		done <- err
	}()
	<-entered
	ops := make(chan struct{})
	go func() {
		a.AddAll("b", []Room{"b", "red"})
		_ = a.SocketRooms("a")
		_, _ = a.Sockets(context.Background(), nil)
		_, _ = a.FetchSockets(context.Background(), BroadcastOptions{})
		a.Del("b", "red")
		a.DelAll("b")
		close(ops)
	}()
	select {
	case <-ops:
	case <-time.After(5 * time.Second):
		t.Fatal("membership operations blocked while a recipient was in Deliver")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestMemoryAdapterConcurrent runs joins, leaves, broadcasts and queries together under
// -race and checks the membership they leave behind.
func TestMemoryAdapterConcurrent(t *testing.T) {
	const workers, rounds = 8, 200
	local := newFakeLocal()
	a := newMemoryAdapter(local)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(2)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				sid := SocketID(fmt.Sprintf("s%d-%d", w, i%4))
				local.setLive(sid, `{}`, true)
				a.AddAll(sid, []Room{sid, "shared", Room(fmt.Sprintf("r%d", i%3))})
				a.Del(sid, Room(fmt.Sprintf("r%d", (i+1)%3)))
				if i%2 == 1 {
					a.DelAll(sid)
					local.setLive(sid, "", false)
				}
			}
			for i := 0; i < 4; i++ {
				a.DelAll(SocketID(fmt.Sprintf("s%d-%d", w, i)))
			}
			a.AddAll(SocketID(fmt.Sprintf("keep%d", w)), []Room{"shared"})
		}(w)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				_, _ = a.Broadcast(context.Background(), testPacket, BroadcastOptions{Rooms: []Room{"shared", "r1"}, Except: []Room{"r2"}})
				_, _ = a.Sockets(context.Background(), []Room{"shared"})
				_, _ = a.FetchSockets(context.Background(), BroadcastOptions{Except: []Room{"r0"}})
				_ = a.SocketRooms("s0-0")
			}
		}()
	}
	wg.Wait()
	for w := 0; w < workers; w++ {
		for i := 0; i < 4; i++ {
			if rooms := a.SocketRooms(SocketID(fmt.Sprintf("s%d-%d", w, i))); rooms != nil {
				t.Errorf("s%d-%d left with rooms %v", w, i, rooms)
			}
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.rooms) != 1 || len(a.rooms["shared"]) != workers || len(a.sids) != workers {
		t.Errorf("membership left behind: %d rooms, %d in shared, %d sockets; want only the %d keep sockets in shared",
			len(a.rooms), len(a.rooms["shared"]), len(a.sids), workers)
	}
}

func TestMemoryAdapterCloseAndServerSideEmit(t *testing.T) {
	local := newFakeLocal("a")
	a := newMemoryAdapter(local)
	a.AddAll("a", []Room{"a"})
	if err := a.ServerSideEmit(context.Background(), "ping", 1); err != nil || len(local.attempted()) != 0 {
		t.Fatalf("ServerSideEmit = %v with %d deliveries; want nil and none", err, len(local.attempted()))
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	res, err := a.Broadcast(context.Background(), testPacket, BroadcastOptions{})
	if err != nil || res.LocalRecipients != 1 {
		t.Fatalf("Broadcast after Close = %+v, %v; want the adapter to keep working", res, err)
	}
}

func TestMemoryAdapterFactoryNeedsLocalSockets(t *testing.T) {
	for _, nsp := range []*Namespace{nil, {name: "/chat"}} {
		ad, err := memoryAdapterFactory(context.Background(), nsp)
		if ad != nil || err == nil {
			t.Errorf("factory(%q) = %v, %v; want no adapter and an error", nsp.Name(), ad, err)
		}
	}
}
