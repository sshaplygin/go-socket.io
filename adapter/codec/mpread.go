package codec

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MessagePack format codes (the spec's names) used by the reader.
const (
	mpNil     = 0xc0
	mpFalse   = 0xc2
	mpTrue    = 0xc3
	mpBin8    = 0xc4
	mpBin16   = 0xc5
	mpBin32   = 0xc6
	mpFloat32 = 0xca
	mpFloat64 = 0xcb
	mpUint8   = 0xcc
	mpUint16  = 0xcd
	mpUint32  = 0xce
	mpUint64  = 0xcf
	mpInt8    = 0xd0
	mpInt16   = 0xd1
	mpInt32   = 0xd2
	mpInt64   = 0xd3
	mpStr8    = 0xd9
	mpStr16   = 0xda
	mpStr32   = 0xdb
	mpArray16 = 0xdc
	mpArray32 = 0xdd
	mpMap16   = 0xde
	mpMap32   = 0xdf
)

// mpReader converts one MessagePack message to JSON text without building an
// intermediate tree. It reads from a slice whose length is already bounded by
// Limits.MaxMessageBytes and checks every declared length against the bytes that are
// left, so a 5-byte header cannot make it allocate gigabytes. Binary values are
// replaced by Socket.IO placeholders and collected in atts when allowBinary is set.
type mpReader struct {
	b           []byte
	pos         int
	lim         Limits
	allowBinary bool
	out         []byte
	atts        [][]byte
}

func (r *mpReader) fail(format string, args ...any) error {
	return fmt.Errorf("%w: at byte %d: %s", ErrMalformed, r.pos, fmt.Sprintf(format, args...))
}

func (r *mpReader) take(n int) ([]byte, error) {
	if n < 0 || n > len(r.b)-r.pos {
		return nil, r.fail("truncated: need %d bytes, %d left", n, len(r.b)-r.pos)
	}
	s := r.b[r.pos : r.pos+n]
	r.pos += n
	return s, nil
}

func (r *mpReader) uintN(n int) (uint64, error) {
	s, err := r.take(n)
	if err != nil {
		return 0, err
	}
	switch n {
	case 1:
		return uint64(s[0]), nil
	case 2:
		return uint64(binary.BigEndian.Uint16(s)), nil
	case 4:
		return uint64(binary.BigEndian.Uint32(s)), nil
	}
	return binary.BigEndian.Uint64(s), nil
}

// length returns the element count (arrays, maps) or byte length (str, bin) that
// follows code c, which must belong to the family kind (see family).
func (r *mpReader) length(c byte, kind byte) (int, error) {
	var width int
	switch kind {
	case 's':
		switch {
		case c >= 0xa0 && c <= 0xbf:
			return int(c & 0x1f), nil
		case c == mpStr8:
			width = 1
		case c == mpStr16:
			width = 2
		case c == mpStr32:
			width = 4
		default:
			return 0, r.fail("code 0x%02x is not a %c value", c, kind)
		}
	case 'b':
		switch c {
		case mpBin8:
			width = 1
		case mpBin16:
			width = 2
		case mpBin32:
			width = 4
		default:
			return 0, r.fail("code 0x%02x is not a %c value", c, kind)
		}
	case 'a':
		switch {
		case c >= 0x90 && c <= 0x9f:
			return int(c & 0x0f), nil
		case c == mpArray16:
			width = 2
		case c == mpArray32:
			width = 4
		default:
			return 0, r.fail("code 0x%02x is not a %c value", c, kind)
		}
	default: // 'm'
		switch {
		case c >= 0x80 && c <= 0x8f:
			return int(c & 0x0f), nil
		case c == mpMap16:
			width = 2
		case c == mpMap32:
			width = 4
		default:
			return 0, r.fail("code 0x%02x is not a %c value", c, kind)
		}
	}
	n, err := r.uintN(width)
	if err != nil {
		return 0, err
	}
	if n > uint64(len(r.b)) {
		return 0, r.fail("declared length %d exceeds the message", n)
	}
	return int(n), nil
}

func (r *mpReader) code() (byte, error) {
	s, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return s[0], nil
}

// readString reads a str value.
func (r *mpReader) readString() (string, error) {
	c, err := r.code()
	if err != nil {
		return "", err
	}
	if family(c) != 's' {
		return "", r.fail("expected a string, got code 0x%02x", c)
	}
	n, err := r.length(c, 's')
	if err != nil {
		return "", err
	}
	return r.str(n)
}

// str takes n bytes and requires valid UTF-8.
func (r *mpReader) str(n int) (string, error) {
	s, err := r.take(n)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(s) {
		return "", r.fail("string is not valid UTF-8")
	}
	return string(s), nil
}

// readArrayLen reads an array header and returns its element count.
func (r *mpReader) readArrayLen() (int, error) {
	c, err := r.code()
	if err != nil {
		return 0, err
	}
	if family(c) != 'a' {
		return 0, r.fail("expected an array, got code 0x%02x", c)
	}
	return r.length(c, 'a')
}

// value appends the JSON text of the next value to r.out.
func (r *mpReader) value(depth int) error {
	if depth > r.lim.MaxDepth {
		return fmt.Errorf("%w: nesting deeper than %d", ErrLimit, r.lim.MaxDepth)
	}
	c, err := r.code()
	if err != nil {
		return err
	}
	switch {
	case c <= 0x7f:
		r.out = strconv.AppendUint(r.out, uint64(c), 10)
		return nil
	case c >= 0xe0:
		r.out = strconv.AppendInt(r.out, int64(int8(c)), 10)
		return nil
	}
	switch c {
	case mpNil:
		r.out = append(r.out, "null"...)
		return nil
	case mpFalse:
		r.out = append(r.out, "false"...)
		return nil
	case mpTrue:
		r.out = append(r.out, "true"...)
		return nil
	case mpUint8, mpUint16, mpUint32, mpUint64:
		n, err := r.uintN(1 << (c - mpUint8))
		if err != nil {
			return err
		}
		r.out = strconv.AppendUint(r.out, n, 10)
		return nil
	case mpInt8, mpInt16, mpInt32, mpInt64:
		w := 1 << (c - mpInt8)
		n, err := r.uintN(w)
		if err != nil {
			return err
		}
		r.out = strconv.AppendInt(r.out, int64(n<<(64-8*w))>>(64-8*w), 10)
		return nil
	case mpFloat32:
		n, err := r.uintN(4)
		if err != nil {
			return err
		}
		return r.float(float64(math.Float32frombits(uint32(n))), 32)
	case mpFloat64:
		n, err := r.uintN(8)
		if err != nil {
			return err
		}
		return r.float(math.Float64frombits(n), 64)
	}
	switch fam := family(c); fam {
	case 's', 'b', 'a', 'm':
		n, err := r.length(c, fam)
		if err != nil {
			return err
		}
		switch fam {
		case 's':
			s, err := r.str(n)
			if err != nil {
				return err
			}
			r.out = appendJSONString(r.out, s)
			return nil
		case 'b':
			return r.binary(n)
		case 'a':
			return r.array(n, depth)
		}
		return r.object(n, depth)
	}
	return r.fail("unsupported MessagePack code 0x%02x (extension types are not part of the format)", c)
}

// family classifies the length-prefixed codes: string, binary, array, map; 0 otherwise.
func family(c byte) byte {
	switch {
	case c >= 0xa0 && c <= 0xbf, c >= mpStr8 && c <= mpStr32:
		return 's'
	case c >= mpBin8 && c <= mpBin32:
		return 'b'
	case c >= 0x90 && c <= 0x9f, c == mpArray16, c == mpArray32:
		return 'a'
	case c >= 0x80 && c <= 0x8f, c == mpMap16, c == mpMap32:
		return 'm'
	}
	return 0
}

func (r *mpReader) float(f float64, bits int) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return r.fail("non-finite number")
	}
	var b []byte
	var err error
	if bits == 32 {
		b, err = json.Marshal(float32(f))
	} else {
		b, err = json.Marshal(f)
	}
	if err != nil {
		return r.fail("number: %v", err)
	}
	r.out = append(r.out, b...)
	return nil
}

func (r *mpReader) binary(n int) error {
	if !r.allowBinary {
		return r.fail("binary value where none is allowed")
	}
	if len(r.atts) >= r.lim.MaxAttachments {
		return fmt.Errorf("%w: more than %d binary attachments", ErrLimit, r.lim.MaxAttachments)
	}
	s, err := r.take(n)
	if err != nil {
		return err
	}
	// make, not append to nil: an empty attachment must stay a non-nil empty slice.
	att := make([]byte, n)
	copy(att, s)
	r.out = append(r.out, `{"_placeholder":true,"num":`...)
	r.out = strconv.AppendInt(r.out, int64(len(r.atts)), 10)
	r.out = append(r.out, '}')
	r.atts = append(r.atts, att)
	return nil
}

func (r *mpReader) array(n, depth int) error {
	if n > len(r.b)-r.pos { // every element takes at least one byte
		return r.fail("array of %d elements exceeds the message", n)
	}
	r.out = append(r.out, '[')
	for i := 0; i < n; i++ {
		if i > 0 {
			r.out = append(r.out, ',')
		}
		if err := r.value(depth + 1); err != nil {
			return err
		}
	}
	r.out = append(r.out, ']')
	return nil
}

func (r *mpReader) object(n, depth int) error {
	if 2*n > len(r.b)-r.pos { // a key and a value take at least two bytes
		return r.fail("map of %d entries exceeds the message", n)
	}
	start := len(r.out)
	r.out = append(r.out, '{')
	for i := 0; i < n; i++ {
		if i > 0 {
			r.out = append(r.out, ',')
		}
		key, err := r.readString()
		if err != nil {
			return err
		}
		r.out = appendJSONString(r.out, key)
		r.out = append(r.out, ':')
		if err := r.value(depth + 1); err != nil {
			return err
		}
	}
	r.out = append(r.out, '}')
	if n == 2 && looksLikePlaceholder(r.out[start:]) {
		// A real binary value became a placeholder in binary(); a map of this shape
		// would read back as one and could not be told from it.
		return r.fail("map shaped like a binary placeholder")
	}
	return nil
}

// looksLikePlaceholder reports whether the compact JSON object j is
// {"_placeholder":true,"num":N} or {"num":N,"_placeholder":true} with N digits only.
func looksLikePlaceholder(j []byte) bool {
	const a, b = `{"_placeholder":true,"num":`, `,"_placeholder":true}`
	digits := func(d []byte) bool {
		if len(d) == 0 {
			return false
		}
		for _, c := range d {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	s := string(j)
	if strings.HasPrefix(s, a) && strings.HasSuffix(s, "}") {
		return digits(j[len(a) : len(j)-1])
	}
	if strings.HasPrefix(s, `{"num":`) && strings.HasSuffix(s, b) {
		return digits(j[len(`{"num":`) : len(j)-len(b)])
	}
	return false
}

const hexDigits = "0123456789abcdef"

// appendJSONString appends s as a JSON string literal. s must be valid UTF-8. Unlike
// encoding/json it does not escape <, > and & (the JavaScript JSON.stringify does not).
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			dst = append(dst, '\\', c)
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"')
}
