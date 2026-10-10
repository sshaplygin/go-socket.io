package parser

import "time"

// Assembler turns the frames of one connection into packets. A connection delivers
// one text frame per envelope, and after an envelope that announces attachments, one
// binary frame per attachment, in order. An Assembler is the incremental form of
// Decode and returns what Decode returns for the same message.
//
// It holds at most one incomplete message, bounded by Limits: the envelope is
// validated when it arrives, so an invalid message is rejected before any attachment
// is awaited, and the bytes of the envelope and of the attachments received so far
// never exceed MaxEventBytes. An Assembler does not read the clock on its own
// initiative: the runtime arms a timer for Deadline and calls Reset or rejects the
// connection when it fires; Binary also rejects a frame that arrives after the
// deadline. Any error leaves the Assembler empty. It is not safe for concurrent use.
type Assembler struct {
	l       Limits
	now     func() time.Time
	pending *pendingMessage
}

type pendingMessage struct {
	packet   Packet
	want     int
	bytes    int
	deadline time.Time
}

// NewAssembler returns an Assembler that enforces limits (zero fields select the
// defaults) or ErrLimit.
func NewAssembler(limits Limits) (*Assembler, error) {
	l, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	return &Assembler{l: l, now: time.Now}, nil
}

// Text accepts a text frame, an envelope. It returns the packet and true when the
// message is complete, which is at once for an envelope without attachments. For a
// binary message it returns false and the Assembler waits for Binary frames; a text
// frame while a message is pending is ErrUnexpectedFrame. text is not retained.
func (a *Assembler) Text(text []byte) (Packet, bool, error) {
	if a.pending != nil {
		a.Reset()
		return Packet{}, false, ErrUnexpectedFrame
	}
	p, n, err := parseEnvelope(text, a.l)
	if err != nil {
		return Packet{}, false, err
	}
	if n == 0 {
		return p, true, nil
	}
	a.pending = &pendingMessage{
		packet:   p,
		want:     n,
		bytes:    len(text),
		deadline: a.now().Add(a.l.AttachmentTimeout),
	}
	return Packet{}, false, nil
}

// Binary accepts a binary frame, the next attachment of the pending message. It
// returns the packet and true with the last attachment. A frame with no message
// pending is ErrUnexpectedFrame, one that arrives after Deadline is
// ErrAttachmentTimeout, and one that takes the message past MaxEventBytes is
// ErrTooLarge. frame is copied.
func (a *Assembler) Binary(frame []byte) (Packet, bool, error) {
	m := a.pending
	if m == nil {
		return Packet{}, false, ErrUnexpectedFrame
	}
	if a.now().After(m.deadline) {
		a.Reset()
		return Packet{}, false, ErrAttachmentTimeout
	}
	if len(frame) > a.l.MaxEventBytes-m.bytes {
		a.Reset()
		return Packet{}, false, ErrTooLarge
	}
	m.bytes += len(frame)
	m.packet.Attachments = append(m.packet.Attachments, append([]byte{}, frame...))
	if len(m.packet.Attachments) < m.want {
		return Packet{}, false, nil
	}
	a.pending = nil
	return m.packet, true, nil
}

// Pending reports whether a message waits for attachments.
func (a *Assembler) Pending() bool { return a.pending != nil }

// Deadline returns the time by which the pending message must be complete, and false
// when none is pending.
func (a *Assembler) Deadline() (time.Time, bool) {
	if a.pending == nil {
		return time.Time{}, false
	}
	return a.pending.deadline, true
}

// Reset discards the pending message, if any, with its attachments.
func (a *Assembler) Reset() { a.pending = nil }
