package parser

import (
	"bytes"
	"strconv"
)

// finishAttachments retains only Buffers present in the final JSON, after
// encoding/json applies omission rules and user-defined IsZero methods. The
// private marker distinguishes numbered Buffers from user-supplied objects that
// happen to have the wire placeholder shape. It is removed before transmission.
func finishAttachments(data []byte, buffers [][]byte, marker string) ([]byte, [][]byte, error) {
	if len(buffers) == 0 {
		return data, nil, nil
	}
	prefix := []byte(`{"_placeholder":true,"num":`)
	suffix := []byte(`,"_socketio_buffer":"` + marker + `"}`)
	type reference struct{ start, end, index int }
	var refs []reference
	used := make([]bool, len(buffers))
	for offset := 0; offset < len(data); {
		at := bytes.Index(data[offset:], prefix)
		if at < 0 {
			break
		}
		start := offset + at
		numberStart := start + len(prefix)
		end := numberStart
		for end < len(data) && data[end] >= '0' && data[end] <= '9' {
			end++
		}
		offset = end
		if !bytes.HasPrefix(data[end:], suffix) {
			continue
		}
		index, err := strconv.ParseUint(string(data[numberStart:end]), 10, 64)
		if err != nil || index >= uint64(len(buffers)) {
			return nil, nil, errIllegalAttachments
		}
		offset = end + len(suffix)
		refs = append(refs, reference{start, offset, int(index)})
		used[index] = true
	}
	var retained [][]byte
	numbers := make([]int, len(buffers))
	for i, keep := range used {
		if keep {
			numbers[i] = len(retained)
			retained = append(retained, buffers[i])
		}
	}
	if len(retained) > maxAttachments {
		return nil, nil, errTooManyAttachments
	}
	if len(refs) == 0 {
		return data, nil, nil
	}
	out := make([]byte, 0, len(data))
	previous := 0
	for _, ref := range refs {
		out = append(out, data[previous:ref.start]...)
		out = append(out, prefix...)
		out = strconv.AppendInt(out, int64(numbers[ref.index]), 10)
		out = append(out, '}')
		previous = ref.end
	}
	out = append(out, data[previous:]...)
	return out, retained, nil
}
