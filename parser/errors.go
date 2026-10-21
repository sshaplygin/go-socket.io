package parser

import "errors"

var (
	errTooManyAttachments = errors.New("too many attachments")
	errIllegalAttachments = errors.New("illegal attachments")
	errInvalidPayload     = errors.New("invalid payload")

	ErrInvalidPacketType = errors.New("invalid packet type")

	errInvalidBinaryBufferType = errors.New("buffer packet should be binary")

	errInvalidFirstPacketType = errors.New("first packet should be text frame")

	errFailedBufferAddress = errors.New("can't get Buffer address")
)
