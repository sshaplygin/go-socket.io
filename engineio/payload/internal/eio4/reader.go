package eio4

import (
	"fmt"
	"io"
)

// DecodeReader reads and decodes one complete polling body with Decode's packet
// and error semantics. maxBytes must be positive and counts wire bytes, including
// separators and base64 expansion. It is independent of HTTP Content-Length.
//
// The body is buffered up to maxBytes. At that boundary one additional byte is
// read to distinguish an exact-sized body from an oversized one. ErrTooLarge is
// returned as soon as that byte is received, without draining the remaining body.
// No packets are returned after a read error; errors.Is can inspect its cause.
// The caller owns closing r, read deadlines, cancellation, and mapping errors to
// HTTP responses (in particular, ErrTooLarge to status 413 for an incoming POST).
// Like Decode, this is a complete-batch decoder, not an incremental consumer.
func DecodeReader(r io.Reader, maxBytes int) ([]Packet, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	// Do not add one to maxBytes: callers may pass the largest positive int.
	limited := &io.LimitedReader{R: r, N: int64(maxBytes)}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("eio4: read polling body: %w", err)
	}
	if limited.N == 0 {
		var extra [1]byte
		n, err := io.ReadFull(r, extra[:])
		if n != 0 {
			return nil, ErrTooLarge
		}
		if err != io.EOF {
			return nil, fmt.Errorf("eio4: read polling body: %w", err)
		}
	}
	return Decode(body, maxBytes)
}
