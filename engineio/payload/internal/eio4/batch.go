package eio4

import "errors"

// EncodeBatch encodes the longest consecutive prefix that fits maxBytes wire
// bytes, counting type bytes, separators and exact base64 expansion. It returns
// that prefix's packet count. maxBytes must be positive; an empty queue returns
// nil, 0, nil. Packets are never split, skipped or reordered.
// For client polling POSTs, use the peer's advertised maxPayload. This helper
// does not impose that inbound server limit on server-to-client responses.
//
// If the first packet cannot fit, EncodeBatch returns ErrTooLarge. Unlike the JS
// client's estimated batching, it never emits a body larger than maxBytes. An
// invalid packet found while scanning fails the whole call with no body or count.
// Scanning stops at the first size rejection; later packets are not examined.
//
// The input queue is unchanged and the returned body owns its bytes. The caller
// must keep the selected packets queued until the transport confirms the write,
// then remove exactly count packets; this function does not acknowledge delivery.
func EncodeBatch(packets []Packet, maxBytes int) (body []byte, count int, err error) {
	if maxBytes <= 0 {
		return nil, 0, ErrInvalidLimit
	}
	if len(packets) == 0 {
		return nil, 0, nil
	}
	count, size, err := measure(packets, maxBytes)
	if err != nil && (count == 0 || !errors.Is(err, ErrTooLarge)) {
		return nil, 0, err
	}
	return encodePackets(packets[:count], size), count, nil
}
