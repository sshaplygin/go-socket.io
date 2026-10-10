package parser

import "time"

// Defaults of Limits. They equal the defaults of the matching socketio.Options
// fields (docs/ROADMAP.md, 2.3 Resource limits).
const (
	DefaultMaxEventBytes     = 1 << 20
	DefaultMaxAttachments    = 64
	DefaultMaxDepth          = 64
	DefaultAttachmentTimeout = 10 * time.Second
)

// Limits bounds one message. A zero field selects its default, never an unlimited
// value; a negative field is invalid and every call that takes Limits returns
// ErrLimit for it.
type Limits struct {
	// MaxEventBytes bounds the UTF-8 bytes of the envelope plus the bytes of all its
	// attachments. It is the socketio.Options.MaxEventBytes value.
	MaxEventBytes int
	// MaxAttachments bounds the attachments of one message. It is the
	// socketio.Options.MaxAttachments value.
	MaxAttachments int
	// MaxDepth bounds the nesting of JSON arrays and objects.
	MaxDepth int
	// AttachmentTimeout bounds the time from the envelope of a binary message to its
	// last attachment. Only Assembler uses it. It is the
	// socketio.Options.AttachmentTimeout value.
	AttachmentTimeout time.Duration
}

func (l Limits) normalized() (Limits, error) {
	if l.MaxEventBytes < 0 || l.MaxAttachments < 0 || l.MaxDepth < 0 || l.AttachmentTimeout < 0 {
		return l, ErrLimit
	}
	if l.MaxEventBytes == 0 {
		l.MaxEventBytes = DefaultMaxEventBytes
	}
	if l.MaxAttachments == 0 {
		l.MaxAttachments = DefaultMaxAttachments
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	if l.AttachmentTimeout == 0 {
		l.AttachmentTimeout = DefaultAttachmentTimeout
	}
	return l, nil
}
