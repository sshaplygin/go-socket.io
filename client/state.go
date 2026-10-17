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
	observersMu     sync.RWMutex
	observers       clientObservers
	mu              sync.Mutex
	client          *Client
	conn            *conn
	sockets         map[string]*Socket
	order           []*Socket
	attempt         *connectAttempt
	retry           *reconnectRun
	reconnectWorker *reconnectRun
	stopped         bool
	closingAll      bool
	rootReady       bool
	buffered        int
}

func (m *clientState) connect(c *Client) (result error) {
	return m.open(c, nil, 0)
}

func (m *clientState) open(c *Client, retry *reconnectRun, number int) (result error) {
	m.mu.Lock()
	if retry != nil && (m.retry != retry || m.stopped) {
		m.mu.Unlock()
		return io.ErrClosedPipe
	}
	if attempt := m.attempt; attempt != nil {
		m.mu.Unlock()
		<-attempt.finished
		return attempt.err
	}
	if m.conn != nil && !isDone(m.conn.closing) {
		m.mu.Unlock()
		return nil
	}
	if retry == nil {
		m.cancelRetryLocked()
	}
	previous := m.conn
	previousWorker := m.reconnectWorker
	a := &connectAttempt{stop: make(chan struct{}), finished: make(chan struct{})}
	m.attempt, m.stopped, m.closingAll = a, false, false
	// An explicit Connect starts a new lifecycle. A Socket.Close after this
	// point must remain effective when this attempt's dial eventually returns.
	if retry == nil {
		for _, socket := range m.order {
			socket.active = true
		}
	}
	m.mu.Unlock()
	finishAttempt := func() {
		a.cancel()
		m.mu.Lock()
		if m.attempt == a {
			a.err = result
			m.attempt = nil
			close(a.finished)
		}
		m.mu.Unlock()
	}
	defer finishAttempt()
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
	// An inactive root has unsubscribed from manager lifecycle notifications.
	// Keep the cache for an active root or one whose Socket does not exist yet.
	if root := m.sockets[rootNamespace]; root == nil || root.active {
		if err := current.connectClient(); err != nil {
			m.mu.Unlock()
			current.connected(err)
			return err
		}
	}
	m.conn, c.conn, m.rootReady = current, current, false
	for _, socket := range m.order {
		if !socket.active {
			continue
		}
		socket.connected, socket.requested = false, false
		socket.nc, socket.connectPending, socket.connectQueued = nil, false, false
	}
	m.mu.Unlock()
	current.connected(nil)
	// Release callers joining this attempt before invoking a callback that may
	// itself call Connect or Close. No connection workers have started yet.
	if retry != nil {
		finishAttempt()
		if f := c.observerSnapshot().reconnect; f != nil {
			f(number)
		}
	}
	m.mu.Lock()
	if m.conn == current && !m.stopped {
		for _, socket := range m.order {
			if !socket.active || socket.requested {
				continue
			}
			socket.requested = true
			if socket.namespace != rootNamespace {
				current.writeControl(socketConnectHeader(socket))
			}
		}
	}
	if retry != nil && m.retry == retry {
		m.retry = nil
	}
	m.mu.Unlock()
	start := func() {
		go current.serveError()
		go current.serveWrite()
		go current.serveRead(clientConnectPacketHandler, clientDisconnectPacketHandler)
		go m.connectionEnded(current)
	}
	if previous != nil || previousWorker != nil {
		go func() {
			if previous != nil {
				<-previous.readStopped
				<-previous.writeStopped
				<-previous.errorStopped
			}
			if previousWorker != nil {
				<-previousWorker.done
			}
			start()
		}()
	} else {
		start()
	}
	return nil
}
