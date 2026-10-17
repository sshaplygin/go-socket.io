package client

import (
	"io"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
)

type connectAttempt struct {
	stop     chan struct{}
	finished chan struct{}
	once     sync.Once
	err      error
}

func (a *connectAttempt) cancel() { a.once.Do(func() { close(a.stop) }) }

type clientState struct {
	mu         sync.Mutex
	client     *Client
	conn       *conn
	sockets    map[string]*Socket
	order      []*Socket
	attempt    *connectAttempt
	stopped    bool
	closingAll bool
	rootReady  bool
	buffered   int
}

func (m *clientState) connect(c *Client) (result error) {
	m.mu.Lock()
	if attempt := m.attempt; attempt != nil {
		m.mu.Unlock()
		<-attempt.finished
		return attempt.err
	}
	if m.conn != nil && !isDone(m.conn.closing) {
		m.mu.Unlock()
		return nil
	}
	previous := m.conn
	a := &connectAttempt{stop: make(chan struct{}), finished: make(chan struct{})}
	m.attempt, m.stopped, m.closingAll = a, false, false
	// An explicit Connect starts a new lifecycle. A Socket.Close after this
	// point must remain effective when this attempt's dial eventually returns.
	for _, socket := range m.order {
		socket.active = true
	}
	m.mu.Unlock()
	defer func() {
		a.cancel()
		m.mu.Lock()
		a.err = result
		m.attempt = nil
		close(a.finished)
		m.mu.Unlock()
	}()
	type dialResult struct {
		conn engineio.Conn
		err  error
	}
	dialed := make(chan dialResult)
	go func() {
		conn, err := c.engineDial()
		select {
		case dialed <- dialResult{conn, err}:
		case <-a.stop:
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()
	var timeout <-chan time.Time
	if c.options.Timeout > 0 {
		timer := time.NewTimer(c.options.Timeout)
		defer timer.Stop()
		timeout = timer.C
	}
	var engine engineio.Conn
	select {
	case <-a.stop:
		return io.ErrClosedPipe
	case <-timeout:
		return ErrConnectTimeout
	case dial := <-dialed:
		if dial.err != nil {
			return dial.err
		}
		engine = dial.conn
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		_ = engine.Close()
		return io.ErrClosedPipe
	}
	current := newConn(engine, c.handlers, c.limits, c.log.With("sid", engine.ID()))
	current.owner = m
	if err := current.connectClient(); err != nil {
		m.mu.Unlock()
		current.connected(err)
		return err
	}
	m.conn, c.conn, m.rootReady = current, current, false
	for _, socket := range m.order {
		if !socket.active {
			continue
		}
		socket.connected, socket.requested = false, true
		socket.nc, socket.connectPending, socket.connectQueued = nil, false, false
		if socket.namespace != rootNamespace {
			current.writeControl(socketConnectHeader(socket))
		}
	}
	m.mu.Unlock()
	current.connected(nil)
	start := func() {
		go current.serveError()
		go current.serveWrite()
		go current.serveRead(clientConnectPacketHandler, clientDisconnectPacketHandler)
	}
	if previous != nil {
		go func() {
			<-previous.readStopped
			<-previous.writeStopped
			<-previous.errorStopped
			start()
		}()
	} else {
		start()
	}
	return nil
}
