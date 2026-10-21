package redis

import "errors"

var errMalformedMessagePack = errors.New("invalid redis messagepack structure")

// validateMessagePack checks declared lengths and nesting before the decoder
// allocates containers. Every array element or map entry must have bytes in the
// input; a tiny truncated message cannot request an arbitrarily large map.
func validateMessagePack(data []byte) error {
	remaining, ok := scanMessagePack(data, 0)
	if !ok || len(remaining) != 0 {
		return errMalformedMessagePack
	}
	return nil
}

func scanMessagePack(data []byte, depth int) ([]byte, bool) {
	if len(data) == 0 || depth > 1000 {
		return nil, false
	}
	code := data[0]
	data = data[1:]
	var size, items uint64
	var lengthBytes int
	var container, mapping, extension bool
	switch {
	case code <= 0x7f || code >= 0xe0 || code == 0xc0 || code == 0xc2 || code == 0xc3:
		return data, true
	case code >= 0xa0 && code <= 0xbf:
		size = uint64(code & 0x1f)
	case code >= 0x90 && code <= 0x9f:
		container, items = true, uint64(code&0x0f)
	case code >= 0x80 && code <= 0x8f:
		container, items = true, uint64(code&0x0f)*2
	case code == 0xc4 || code == 0xd9 || code == 0xc7:
		lengthBytes = 1
		extension = code == 0xc7
	case code == 0xc5 || code == 0xda || code == 0xc8:
		lengthBytes = 2
		extension = code == 0xc8
	case code == 0xc6 || code == 0xdb || code == 0xc9:
		lengthBytes = 4
		extension = code == 0xc9
	case code == 0xdc || code == 0xde:
		lengthBytes, container, mapping = 2, true, code == 0xde
	case code == 0xdd || code == 0xdf:
		lengthBytes, container, mapping = 4, true, code == 0xdf
	case code == 0xcc || code == 0xd0:
		size = 1
	case code == 0xcd || code == 0xd1:
		size = 2
	case code == 0xce || code == 0xd2 || code == 0xca:
		size = 4
	case code == 0xcf || code == 0xd3 || code == 0xcb:
		size = 8
	case code >= 0xd4 && code <= 0xd8:
		size = 1 + (uint64(1) << (code - 0xd4))
	default:
		return nil, false
	}
	if lengthBytes != 0 {
		if len(data) < lengthBytes {
			return nil, false
		}
		for _, b := range data[:lengthBytes] {
			size = size<<8 | uint64(b)
		}
		data = data[lengthBytes:]
		if container {
			items = size
			if mapping {
				items *= 2
			}
		} else if extension {
			size++ // extension type byte
		}
	}
	if container {
		if items > uint64(len(data)) {
			return nil, false
		}
		for ; items != 0; items-- {
			var ok bool
			data, ok = scanMessagePack(data, depth+1)
			if !ok {
				return nil, false
			}
		}
		return data, true
	}
	if size > uint64(len(data)) {
		return nil, false
	}
	return data[size:], true
}
