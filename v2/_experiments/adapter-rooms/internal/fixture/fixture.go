// Package fixture checks a trusted Node-generated corpus. It is not an adapter.
package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

type Corpus struct {
	Schema  int    `json:"schema"`
	Adapter string `json:"adapter"`
	Parser  string `json:"parser"`
	Cases   []Case `json:"cases"`
}

type Entry struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

type State struct {
	Rooms []Entry  `json:"rooms"`
	SIDs  []Entry  `json:"sids"`
	Live  []string `json:"live"`
}

type Action struct {
	Op    string   `json:"op"`
	ID    string   `json:"id"`
	Rooms []string `json:"rooms,omitempty"`
	Room  string   `json:"room,omitempty"`
}

type Step struct {
	Action Action `json:"action"`
	State  State  `json:"state"`
}

type Query struct {
	Rooms []string `json:"rooms"`
	IDs   []string `json:"ids"`
}

type SocketRooms struct {
	ID    string   `json:"id"`
	Rooms []string `json:"rooms"`
}

type Write struct {
	ID      string          `json:"id"`
	Packets []string        `json:"packets"`
	Options map[string]bool `json:"options"`
}

type Packet struct {
	Type int             `json:"type"`
	Data json.RawMessage `json:"data"`
	NSP  string          `json:"nsp"`
}

type Notification struct {
	ID     string `json:"id"`
	Packet Packet `json:"packet"`
}

type Broadcast struct {
	Rooms         []string        `json:"rooms"`
	Except        []string        `json:"except"`
	Flags         map[string]bool `json:"flags"`
	Writes        []Write         `json:"writes"`
	Notifications []Notification  `json:"notifications"`
}

type Event struct {
	Event string   `json:"event"`
	Args  []string `json:"args"`
}

type Case struct {
	Name        string        `json:"name"`
	Trace       []Step        `json:"trace"`
	Events      []Event       `json:"events"`
	Queries     []Query       `json:"queries"`
	SocketRooms []SocketRooms `json:"socketRooms"`
	Broadcast   Broadcast     `json:"broadcast"`
}

func Load(r io.Reader) (*Corpus, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var corpus Corpus
	if err := decoder.Decode(&corpus); err != nil {
		return nil, fmt.Errorf("decode corpus: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("corpus must contain exactly one JSON value")
	}
	if err := corpus.Validate(); err != nil {
		return nil, err
	}
	return &corpus, nil
}

func ordered(values []string) bool {
	return slices.IsSorted(values) && len(slices.Compact(slices.Clone(values))) == len(values)
}

func index(entries []Entry, allowEmpty bool) (map[string][]string, error) {
	result := make(map[string][]string, len(entries))
	var keys []string
	for _, entry := range entries {
		if !ordered(entry.Values) || (!allowEmpty && len(entry.Values) == 0) {
			return nil, fmt.Errorf("invalid members for %q", entry.Key)
		}
		keys = append(keys, entry.Key)
		result[entry.Key] = entry.Values
	}
	if !ordered(keys) {
		return nil, fmt.Errorf("noncanonical or duplicate map keys")
	}
	return result, nil
}

func (s State) validate() error {
	rooms, err := index(s.Rooms, false)
	if err != nil {
		return err
	}
	sids, err := index(s.SIDs, true)
	if err != nil {
		return err
	}
	if !ordered(s.Live) {
		return fmt.Errorf("noncanonical live socket set")
	}
	for room, ids := range rooms {
		for _, id := range ids {
			if !slices.Contains(sids[id], room) {
				return fmt.Errorf("missing sid backlink for %q/%q", room, id)
			}
		}
	}
	for id, names := range sids {
		for _, room := range names {
			if !slices.Contains(rooms[room], id) {
				return fmt.Errorf("missing room backlink for %q/%q", room, id)
			}
		}
	}
	return nil
}

// selection checks a static set relation in a snapshot, without implementing
// membership mutation, callback dispatch, queues, locks, or an adapter API.
func (s State) selection(rooms, except []string) []string {
	result := make([]string, 0)
	for _, sid := range s.SIDs {
		included, excluded := len(rooms) == 0, false
		for _, room := range sid.Values {
			included = included || slices.Contains(rooms, room)
			excluded = excluded || slices.Contains(except, room)
		}
		if included && !excluded && slices.Contains(s.Live, sid.Key) {
			result = append(result, sid.Key)
		}
	}
	return result
}

func (c Corpus) Validate() error {
	if c.Schema != 1 || c.Adapter != "2.5.5" || c.Parser != "4.2.7" || len(c.Cases) == 0 {
		return fmt.Errorf("unsupported or empty corpus")
	}
	names := make(map[string]bool)
	for _, scenario := range c.Cases {
		if scenario.Name == "" || names[scenario.Name] {
			return fmt.Errorf("empty or duplicate case name %q", scenario.Name)
		}
		names[scenario.Name] = true
		if err := scenario.validate(); err != nil {
			return fmt.Errorf("case %s: %w", scenario.Name, err)
		}
	}
	return nil
}

func (c Case) validate() error {
	var state State
	for _, step := range c.Trace {
		switch step.Action.Op {
		case "connect", "addAll", "del", "delAll", "unregister":
		default:
			return fmt.Errorf("unknown action %q", step.Action.Op)
		}
		if err := step.State.validate(); err != nil {
			return err
		}
		state = step.State
	}
	for _, query := range c.Queries {
		if !slices.Equal(query.IDs, state.selection(query.Rooms, nil)) {
			return fmt.Errorf("query contradicts membership snapshot")
		}
	}
	sids, err := index(state.SIDs, true)
	if err != nil {
		return err
	}
	var queriedIDs []string
	for _, query := range c.SocketRooms {
		queriedIDs = append(queriedIDs, query.ID)
		rooms, present := sids[query.ID]
		if present != (query.Rooms != nil) || !slices.Equal(rooms, query.Rooms) {
			return fmt.Errorf("socketRooms contradicts membership snapshot")
		}
	}
	if !ordered(queriedIDs) {
		return fmt.Errorf("duplicate or noncanonical socketRooms queries")
	}
	var recipients []string
	for _, write := range c.Broadcast.Writes {
		recipients = append(recipients, write.ID)
		if !slices.Equal(write.Packets, []string{`2/rooms,["notice",{"value":1}]`}) || !write.Options["preEncoded"] {
			return fmt.Errorf("unexpected encoded packet or preEncoded flag")
		}
		for key, value := range write.Options {
			if key != "preEncoded" && key != "volatile" && key != "compress" {
				return fmt.Errorf("unexpected write option %q", key)
			}
			if key != "preEncoded" {
				flag, present := c.Broadcast.Flags[key]
				if !present || value != flag {
					return fmt.Errorf("write option differs from broadcast flag")
				}
			}
		}
		for _, key := range []string{"volatile", "compress"} {
			_, want := c.Broadcast.Flags[key]
			_, got := write.Options[key]
			if want != got {
				return fmt.Errorf("missing forwarded flag %q", key)
			}
		}
	}
	if !slices.Equal(recipients, state.selection(c.Broadcast.Rooms, c.Broadcast.Except)) {
		return fmt.Errorf("broadcast recipients contradict membership snapshot")
	}
	var notified []string
	for _, notification := range c.Broadcast.Notifications {
		notified = append(notified, notification.ID)
		var compact bytes.Buffer
		if err := json.Compact(&compact, notification.Packet.Data); err != nil {
			return fmt.Errorf("invalid notification data: %w", err)
		}
		if notification.Packet.Type != 2 || notification.Packet.NSP != "/rooms" || compact.String() != `["notice",{"value":1}]` {
			return fmt.Errorf("unexpected outgoing notification")
		}
	}
	if !slices.Equal(notified, recipients) {
		return fmt.Errorf("notification recipients differ from writes")
	}
	for _, event := range c.Events {
		want := 1
		switch event.Event {
		case "create-room", "delete-room":
		case "join-room", "leave-room":
			want = 2
		default:
			return fmt.Errorf("unknown lifecycle event")
		}
		if len(event.Args) != want {
			return fmt.Errorf("wrong lifecycle event arity")
		}
	}
	return nil
}
