package parser

import "errors"

// Errors returned by the codec. They are matched with errors.Is; a returned error may
// wrap one of them with detail.
var (
	// ErrInvalid reports a malformed packet: an unknown type, a bad namespace or
	// acknowledgement ID, invalid UTF-8 or JSON, or a payload of the wrong shape. An
	// argument codec also returns it for JSON that does not decode into its type.
	ErrInvalid = errors.New("parser: invalid Socket.IO packet")

	// ErrLimit reports a negative field in Limits.
	ErrLimit = errors.New("parser: invalid limits")

	// ErrTooLarge reports a message past Limits.MaxEventBytes.
	ErrTooLarge = errors.New("parser: message exceeds byte limit")

	// ErrAttachments reports an attachment count that does not match the envelope,
	// or a binary placeholder that names no attachment.
	ErrAttachments = errors.New("parser: invalid attachment count or placeholder")

	// ErrTooManyAttachments reports a message past Limits.MaxAttachments.
	ErrTooManyAttachments = errors.New("parser: too many attachments")

	// ErrDepth reports JSON nesting past Limits.MaxDepth. Text that nests past the
	// 10000 levels encoding/json accepts is ErrDepth too, whatever Limits.MaxDepth is.
	ErrDepth = errors.New("parser: JSON nesting exceeds limit")

	// ErrArity reports a number of positional arguments that a codec does not accept.
	ErrArity = errors.New("parser: wrong number of arguments")

	// ErrUnsupported reports a Go value an argument codec cannot send as JSON with
	// binary attachments.
	ErrUnsupported = errors.New("parser: unsupported value")

	// ErrUnexpectedFrame reports a frame the Assembler cannot accept in its state: a
	// text frame while attachments are pending, or a binary frame with none pending.
	ErrUnexpectedFrame = errors.New("parser: unexpected frame")

	// ErrAttachmentTimeout reports that the attachments of a message did not arrive
	// within Limits.AttachmentTimeout.
	ErrAttachmentTimeout = errors.New("parser: attachment assembly timeout")
)
