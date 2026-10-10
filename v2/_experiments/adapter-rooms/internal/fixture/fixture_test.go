package fixture

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/rooms.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadFixture(t *testing.T) *Corpus {
	t.Helper()
	corpus, err := Load(bytes.NewReader(fixtureBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	return corpus
}

func TestCorpus(t *testing.T) {
	corpus := loadFixture(t)
	if len(corpus.Cases) != 22 {
		t.Fatalf("got %d cases, want 22", len(corpus.Cases))
	}
	want := map[string][]string{
		"empty": {}, "all": {"a", "b", "c"},
		"room-union-deduplicates": {"a", "b", "c"},
		"exclude-room":            {"b"}, "exclude-automatic-socket-room": {"b", "c"},
		"id-is-also-another-sockets-room":           {"c"},
		"leave-final-room-retains-empty-sid":        {"a"},
		"empty-add-creates-empty-sid":               {},
		"membership-without-live-socket-is-skipped": {"a", "c"},
		"stale-live-map-entry-is-skipped":           {"b", "c"},
	}
	for _, scenario := range corpus.Cases {
		expected, exists := want[scenario.Name]
		if !exists {
			continue
		}
		var actual []string
		for _, write := range scenario.Broadcast.Writes {
			actual = append(actual, write.ID)
		}
		if !slices.Equal(actual, expected) {
			t.Errorf("%s: got %v, want %v", scenario.Name, actual, expected)
		}
		delete(want, scenario.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing required cases: %v", want)
	}
}

func TestRejectIncoherentCorpus(t *testing.T) {
	mutations := map[string]func(*Corpus){
		"version":                 func(c *Corpus) { c.Adapter = "latest" },
		"duplicate case":          func(c *Corpus) { c.Cases[1].Name = c.Cases[0].Name },
		"unknown action":          func(c *Corpus) { c.Cases[1].Trace[0].Action.Op = "invented" },
		"missing backlink":        func(c *Corpus) { c.Cases[1].Trace[0].State.SIDs[0].Values = []string{} },
		"duplicate membership":    func(c *Corpus) { c.Cases[1].Trace[0].State.Rooms[0].Values = []string{"a", "a"} },
		"empty room":              func(c *Corpus) { c.Cases[1].Trace[0].State.Rooms[0].Values = []string{} },
		"query mismatch":          func(c *Corpus) { c.Cases[1].Queries[0].IDs = []string{"missing"} },
		"null versus empty rooms": func(c *Corpus) { c.Cases[14].SocketRooms[0].Rooms = nil },
		"duplicate recipient": func(c *Corpus) {
			c.Cases[1].Broadcast.Writes = append(c.Cases[1].Broadcast.Writes, c.Cases[1].Broadcast.Writes[0])
		},
		"missing recipient":           func(c *Corpus) { c.Cases[1].Broadcast.Writes = c.Cases[1].Broadcast.Writes[1:] },
		"packet mismatch":             func(c *Corpus) { c.Cases[1].Broadcast.Writes[0].Packets[0] = "bad" },
		"unexpected local write flag": func(c *Corpus) { c.Cases[18].Broadcast.Writes[0].Options["local"] = true },
		"missing forwarded flag":      func(c *Corpus) { delete(c.Cases[19].Broadcast.Writes[0].Options, "volatile") },
		"wrong forwarded flag":        func(c *Corpus) { c.Cases[19].Broadcast.Writes[0].Options["compress"] = true },
		"notification mismatch":       func(c *Corpus) { c.Cases[1].Broadcast.Notifications[0].ID = "missing" },
		"lifecycle arity":             func(c *Corpus) { c.Cases[1].Events[0].Args = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			corpus := loadFixture(t)
			mutate(corpus)
			if err := corpus.Validate(); err == nil {
				t.Fatal("accepted incoherent corpus")
			}
		})
	}
}

func TestStrictJSON(t *testing.T) {
	data := fixtureBytes(t)
	for name, input := range map[string][]byte{
		"truncated":      data[:len(data)/2],
		"trailing value": append(slices.Clone(data), []byte("{}")...),
		"unknown field":  bytes.Replace(data, []byte(`"schema": 1`), []byte(`"unknown": 1, "schema": 1`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(bytes.NewReader(input)); err == nil {
				t.Fatal("accepted malformed corpus")
			}
		})
	}
	// Typed roundtrip preserves the distinction between absent and empty rooms.
	encoded, err := json.Marshal(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bytes.NewReader(encoded)); err != nil {
		t.Fatal(err)
	}
}
