package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gorilla/websocket"
	framing "github.com/sshaplygin/go-socket.io/experiments/eio4-websocket"
)

const messageLimit = 1 << 20

type snapshot struct {
	Active     int64  `json:"active_connections"`
	Goroutines int    `json:"goroutines"`
	HeapAlloc  uint64 `json:"heap_alloc_bytes"`
	HeapInuse  uint64 `json:"heap_inuse_bytes"`
	TotalAlloc uint64 `json:"total_alloc_bytes"`
	Mallocs    uint64 `json:"mallocs"`
	NumGC      uint32 `json:"gc_cycles"`
}

func serveBackend(backend string, lifetime time.Duration) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	var active atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("gc") == "1" {
			runtime.GC()
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot{active.Load(), runtime.NumGoroutine(), m.HeapAlloc, m.HeapInuse, m.TotalAlloc, m.Mallocs, m.NumGC})
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		if backend == "gorilla" {
			u := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, EnableCompression: false}
			conn, err := u.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			active.Add(1)
			defer active.Add(-1)
			conn.SetReadLimit(messageLimit)
			_ = conn.UnderlyingConn().SetDeadline(time.Now().Add(lifetime))
			for {
				op, data, err := conn.ReadMessage()
				if err != nil || conn.WriteMessage(op, data) != nil {
					return
				}
			}
		}
		raw, buffered, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer raw.Close()
		active.Add(1)
		defer active.Add(-1)
		_ = raw.SetDeadline(time.Now().Add(lifetime))
		conn, err := framing.New(raw, buffered.Reader, false, messageLimit)
		if err != nil {
			return
		}
		for {
			op, data, err := conn.ReadMessage()
			if err != nil || conn.WriteMessage(op, data) != nil {
				return
			}
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if _, err := fmt.Fprintln(os.Stdout, listener.Addr()); err != nil {
		return err
	}
	return server.Serve(listener)
}
