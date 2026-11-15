package parser

import (
	"encoding/json"
	"io"
	"time"
)

// ConnParameters are the session parameters of the OPEN packet, which the
// server sends as JSON with the durations in milliseconds.
type ConnParameters struct {
	// PingInterval is the interval at which the client sends PING
	// ("pingInterval").
	PingInterval time.Duration
	// PingTimeout is how long the server waits for a packet after the ping
	// interval before it closes the session ("pingTimeout").
	PingTimeout time.Duration
	// SID is the session ID ("sid").
	SID string
	// Upgrades lists the transports the session may upgrade to ("upgrades").
	Upgrades []string
}

type jsonParameters struct {
	SID          string   `json:"sid"`
	Upgrades     []string `json:"upgrades"`
	PingInterval int      `json:"pingInterval"`
	PingTimeout  int      `json:"pingTimeout"`
}

// ReadConnParameters reads ConnParameters from r.
func ReadConnParameters(r io.Reader) (ConnParameters, error) {
	var param jsonParameters
	if err := json.NewDecoder(r).Decode(&param); err != nil {
		return ConnParameters{}, err
	}

	return ConnParameters{
		SID:          param.SID,
		Upgrades:     param.Upgrades,
		PingInterval: time.Duration(param.PingInterval) * time.Millisecond,
		PingTimeout:  time.Duration(param.PingTimeout) * time.Millisecond,
	}, nil
}

// WriteTo writes to w with json format.
func (p ConnParameters) WriteTo(w io.Writer) (int64, error) {
	arg := jsonParameters{
		SID:          p.SID,
		Upgrades:     p.Upgrades,
		PingInterval: int(p.PingInterval / time.Millisecond),
		PingTimeout:  int(p.PingTimeout / time.Millisecond),
	}
	writer := writer{
		w: w,
	}
	err := json.NewEncoder(&writer).Encode(arg)
	return writer.i, err
}

type writer struct {
	i int64
	w io.Writer
}

func (w *writer) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.i += int64(n)
	return n, err
}
