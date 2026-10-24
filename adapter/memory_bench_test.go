package adapter_test

import (
	"strconv"
	"testing"

	"github.com/sshaplygin/go-socket.io/adapter"
)

type benchSocket struct {
	adapter.Socket
	id string
}

func (s benchSocket) ID() string                                            { return s.id }
func (benchSocket) Emit(string, ...interface{})                             {}
func (benchSocket) EmitWithOptions(adapter.Options, string, ...interface{}) {}

func BenchmarkMemorySendTo(b *testing.B) {
	for _, name := range []string{"one-room", "all", "three-rooms"} {
		b.Run(name, func(b *testing.B) {
			m := adapter.NewMemory()
			opts := adapter.Options{}
			for i := 0; i < 1000; i++ {
				s := benchSocket{id: strconv.Itoa(i)}
				m.Join(s.id, s)
				if name == "three-rooms" {
					if i < 500 {
						m.Join("a", s)
					}
					if i >= 250 && i < 750 {
						m.Join("b", s)
					}
					if i >= 500 {
						m.Join("c", s)
					}
				} else {
					m.Join("r", s)
				}
			}
			if name == "one-room" {
				opts.Rooms = []string{"r"}
			}
			if name == "three-rooms" {
				opts.Rooms = []string{"a", "b", "c"}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := m.SendTo(opts, "e", "x"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
