package socketio

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/sshaplygin/go-socket.io/v2/parser"
)

// roomsCorpus is the part of testdata/rooms/rooms.json that the Go adapter can be
// compared with; see testdata/rooms/README.md.
type roomsCorpus struct {
	Schema  int    `json:"schema"`
	Adapter string `json:"adapter"`
	Cases   []struct {
		Name  string `json:"name"`
		Trace []struct {
			Action struct {
				Op    string `json:"op"`
				ID    string `json:"id"`
				Rooms []Room `json:"rooms"`
				Room  Room   `json:"room"`
			} `json:"action"`
			State struct {
				SIDs []struct {
					Key    SocketID `json:"key"`
					Values []Room   `json:"values"`
				} `json:"sids"`
			} `json:"state"`
		} `json:"trace"`
		Queries []struct {
			Rooms []Room     `json:"rooms"`
			IDs   []SocketID `json:"ids"`
		} `json:"queries"`
		SocketRooms []struct {
			ID    SocketID `json:"id"`
			Rooms []Room   `json:"rooms"`
		} `json:"socketRooms"`
		Broadcast struct {
			Rooms  []Room          `json:"rooms"`
			Except []Room          `json:"except"`
			Flags  map[string]bool `json:"flags"`
			Writes []struct {
				ID      SocketID `json:"id"`
				Packets []string `json:"packets"`
			} `json:"writes"`
		} `json:"broadcast"`
	} `json:"cases"`
}

// TestMemoryAdapterNodeRooms replays the Node in-memory adapter corpus through the
// memory adapter: membership after every action, sockets and socketRooms queries, the
// recipients of a broadcast (room union, deduplication, exclusions, empty selection,
// Local) and the sockets FetchSockets returns for the same selection.
func TestMemoryAdapterNodeRooms(t *testing.T) {
	raw, err := os.ReadFile("testdata/rooms/rooms.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus roomsCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Schema != 1 || corpus.Adapter != "2.5.5" || len(corpus.Cases) != 22 {
		t.Fatalf("unexpected corpus: schema %d, adapter %s, %d cases", corpus.Schema, corpus.Adapter, len(corpus.Cases))
	}
	ctx := context.Background()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			local := newFakeLocal()
			a := newMemoryAdapter(local)
			known := map[SocketID]bool{"missing": true}
			for step, tr := range c.Trace {
				act := tr.Action
				sid := SocketID(act.ID)
				known[sid] = true
				switch act.Op {
				case "connect":
					local.setLive(sid, `{}`, true)
					a.AddAll(sid, append([]Room{sid}, act.Rooms...))
				case "addAll":
					a.AddAll(sid, act.Rooms)
				case "del":
					a.Del(sid, act.Room)
				case "delAll":
					a.DelAll(sid)
				case "unregister":
					local.setLive(sid, "", false)
				default:
					t.Fatalf("unknown action %q", act.Op)
				}
				want := map[SocketID][]Room{}
				for _, e := range tr.State.SIDs {
					want[e.Key] = e.Values
				}
				for id := range known {
					got := a.SocketRooms(id)
					if w, ok := want[id]; ok != (got != nil) || !slices.Equal(sorted(got), w) {
						t.Fatalf("after action %d %+v: SocketRooms(%q) = %v, Node has %v (recorded %t)", step, act, id, got, w, ok)
					}
				}
			}
			for _, q := range c.Queries {
				got, err := a.Sockets(ctx, q.Rooms)
				if err != nil || !slices.Equal(sorted(got), q.IDs) {
					t.Errorf("Sockets(%q) = %v, %v; Node %v", q.Rooms, got, err, q.IDs)
				}
			}
			for _, q := range c.SocketRooms {
				got := a.SocketRooms(q.ID)
				if (got == nil) != (q.Rooms == nil) || !slices.Equal(sorted(got), q.Rooms) {
					t.Errorf("SocketRooms(%q) = %#v, Node %#v", q.ID, got, q.Rooms)
				}
			}

			b := c.Broadcast
			opts := BroadcastOptions{Rooms: b.Rooms, Except: b.Except, Flags: BroadcastFlags{Local: b.Flags["local"]}}
			text, _, err := parser.Encode(testPacket, parser.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			var want []SocketID
			for _, w := range b.Writes {
				want = append(want, w.ID)
				if !slices.Equal(w.Packets, []string{string(text)}) {
					t.Fatalf("Go encodes the packet as %q, Node wrote %q", text, w.Packets)
				}
			}
			res, err := a.Broadcast(ctx, testPacket, opts)
			got := local.deliveredIDs()
			if !slices.Equal(got, want) {
				t.Errorf("delivered to %v, Node wrote to %v", got, want)
			}
			if res.LocalRecipients != len(want) || res.Published {
				t.Errorf("result %+v, want %d local recipients and no publication", res, len(want))
			}
			for id, pkt := range local.delivered {
				if !reflect.DeepEqual(pkt, testPacket) {
					t.Errorf("%s received %+v", id, pkt)
				}
			}
			// Node skips a recorded socket that is not live; the Go adapter tries it and
			// reports ErrSocketClosed beside the same recipients.
			attempts := local.attempted()
			if len(slices.Compact(sorted(attempts))) != len(attempts) {
				t.Errorf("a socket was tried twice: %v", attempts)
			}
			stale := len(attempts) > len(want)
			if stale != errors.Is(err, ErrSocketClosed) || (!stale && err != nil) {
				t.Errorf("attempts %v, recipients %v, error %v", attempts, want, err)
			}

			snaps, err := a.FetchSockets(ctx, opts)
			if err != nil || len(snaps) != len(want) {
				t.Fatalf("FetchSockets = %d sockets, %v; Node selects %v", len(snaps), err, want)
			}
			slices.SortFunc(snaps, func(x, y RemoteSocket) int { return cmp.Compare(x.ID, y.ID) })
			for i, s := range snaps {
				if s.ID != want[i] || !slices.Equal(sorted(s.Rooms), sorted(a.SocketRooms(s.ID))) {
					t.Errorf("FetchSockets[%d] = %s in %v, want %s in %v", i, s.ID, s.Rooms, want[i], a.SocketRooms(want[i]))
				}
			}
		})
	}
}
