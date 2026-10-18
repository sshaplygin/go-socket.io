package session

import (
	"io"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

func (s *Session) Upgrade(transport string, conn transport.Conn) {
	started := time.Now()
	go s.upgradingAt(transport, conn, started)
}

func (s *Session) upgrading(t string, conn transport.Conn) {
	s.upgradingAt(t, conn, time.Now())
}

func (s *Session) upgradingAt(t string, conn transport.Conn, started time.Time) {
	s.upgradeLocker.Lock()
	if s.closed || s.probe != nil {
		s.upgradeLocker.Unlock()
		_ = conn.Close()
		return
	}
	old := s.conn
	s.probe = conn
	s.upgradeLocker.Unlock()
	defer func() {
		s.upgradeLocker.Lock()
		s.probe = nil
		s.upgradeLocker.Unlock()
	}()
	if c, ok := conn.(interface{ SetReadLimit(int64) }); ok {
		c.SetReadLimit(s.maxMessageSize)
	}

	// Closing the candidate also bounds transports whose deadline methods do not
	// interrupt a pending operation. The mutex orders expiry against the switch.
	var state sync.Mutex
	expired, switched := false, false
	deadline := started.Add(s.upgradeTimeout)
	timer := time.AfterFunc(time.Until(deadline), func() {
		state.Lock()
		defer state.Unlock()
		expired = true
		_ = conn.Close()
	})
	defer timer.Stop()
	defer func() {
		if !switched {
			_ = conn.Close()
		}
	}()
	if err := conn.SetReadDeadline(deadline); err != nil {
		return
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return
	}
	ft, pt, r, err := conn.NextReader()
	if err != nil {
		return
	}
	if pt != packet.PING {
		_ = r.Close()
		return
	}
	w, err := conn.NextWriter(ft, packet.PONG)
	if err != nil {
		_ = r.Close()
		return
	}
	_, copyErr := io.Copy(w, r)
	readErr := r.Close()
	writeErr := w.Close()
	if copyErr != nil || readErr != nil || writeErr != nil {
		return
	}
	if s.hooks != nil && s.hooks.Upgrading != nil {
		s.hooks.Upgrading(s, t)
	}
	p, ok := old.(Pauser)
	if !ok {
		return
	}

	// Polling remains writable throughout the probe. A tick answers only a poll
	// currently waiting for a session writer, so an idle client cannot spin.
	stopChecks := make(chan struct{})
	checksDone := make(chan struct{})
	if poll, ok := old.(interface{ AnswerPoll() bool }); ok {
		go func() {
			defer close(checksDone)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopChecks:
					return
				case <-ticker.C:
					poll.AnswerPoll()
				}
			}
		}()
	} else {
		close(checksDone)
	}
	stopPollChecks := sync.OnceFunc(func() { close(stopChecks); <-checksDone })
	defer stopPollChecks()

	_, pt, r, err = conn.NextReader()
	if err != nil {
		return
	}
	readErr = r.Close()
	if pt != packet.UPGRADE || readErr != nil {
		return
	}

	// The timeout ends when UPGRADE is read. Waiting for an in-flight POST
	// afterwards must not let its timer close the newly selected websocket.
	state.Lock()
	if expired || !timer.Stop() {
		state.Unlock()
		return
	}
	state.Unlock()
	stopPollChecks()
	// Pause only after UPGRADE, waiting for any in-flight POST to finish.
	p.Pause()
	if !s.switchTransport(t, conn) {
		p.Resume()
		return
	}
	switched = true
	if s.hooks != nil && s.hooks.Upgrade != nil {
		s.hooks.Upgrade(s, t)
	}
	if err := s.setDeadline(); err != nil {
		s.observe(reasonTransportError, err)
		_ = s.Close()
	}
	if err := old.Close(); err != nil {
		s.logger().Debug("engineio: close connection failed", "err", err)
	}
}

// switchTransport makes conn, on transport t, the session's connection and
// updates the logger's transport attribute. If the session has been closed,
// conn is closed instead and switchTransport reports false.
func (s *Session) switchTransport(t string, conn transport.Conn) bool {
	s.upgradeLocker.Lock()
	if s.closed {
		s.upgradeLocker.Unlock()
		if err := conn.Close(); err != nil {
			s.logger().Debug("engineio: close connection failed", "err", err)
		}
		return false
	}
	s.conn = conn
	s.transport = t
	s.upgraded = true
	s.log.Store(s.baseLog.With("transport", t))
	s.upgradeLocker.Unlock()
	return true
}

// replaced reports whether conn is no longer the session's connection
// because an upgrade switched to another one. It is false once the session
// is closed, so a closed session's operations fail instead of moving on.
func (s *Session) replaced(conn transport.Conn) bool {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return !s.closed && s.conn != conn
}
