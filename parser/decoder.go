package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const (
	bufferTypeName = "Buffer"
	maxAttachments = 10
)

type FrameReader interface {
	NextReader() (FrameType, io.ReadCloser, error)
}
type byteReader interface {
	io.Reader
	ReadByte() (byte, error)
	UnreadByte() error
}

type Decoder struct {
	r           FrameReader
	lastFrame   io.ReadCloser
	bufferCount uint64
	args        []json.RawMessage
}

func NewDecoder(r FrameReader) *Decoder { return &Decoder{r: r} }
func (d *Decoder) Close() error         { return d.DiscardLast() }
func (d *Decoder) DiscardLast() error {
	if d.lastFrame == nil {
		return nil
	}
	r := d.lastFrame
	d.lastFrame = nil
	return r.Close()
}

func (d *Decoder) DecodeHeader(header *Header, event *string) error {
	if err := d.DiscardLast(); err != nil {
		return err
	}
	*header = Header{}
	*event = ""
	d.bufferCount = 0
	d.args = nil
	ft, r, err := d.r.NextReader()
	if err != nil {
		return err
	}
	d.lastFrame = r
	if ft != Text {
		return errInvalidFirstPacketType
	}
	br := bufio.NewReader(r)
	if err := d.readHeader(br, header); err != nil {
		return err
	}
	raw, err := io.ReadAll(br)
	if err != nil {
		return err
	}
	if err := d.validatePayload(header.Type, bytes.TrimSpace(raw), event); err != nil {
		return err
	}
	if header.Type == binaryEvent || header.Type == binaryAck {
		header.Type -= 3
	}
	return nil
}

// DecodeArgs consumes all attachments before Go type conversion. A type mismatch
// can then be reported without leaving binary frames in front of the next packet.
// A nil types slice decodes every argument into a generic JSON value (binary
// placeholders become *Buffer); an empty non-nil slice discards the arguments.
func (d *Decoder) DecodeArgs(types []reflect.Type) ([]reflect.Value, error) {
	_ = d.DiscardLast()
	count := d.bufferCount
	d.bufferCount = 0
	buffers := make([]Buffer, count)
	for i := range buffers {
		ft, r, err := d.r.NextReader()
		if err != nil {
			return nil, err
		}
		data, err := d.readBuffer(ft, r)
		if err != nil {
			return nil, err
		}
		buffers[i].Data = data
	}
	// A nil type list requests all generic JSON arguments. An empty non-nil
	// list consumes the packet without passing arguments to a zero-argument handler.
	if types == nil {
		types = make([]reflect.Type, len(d.args))
		for i := range types {
			types[i] = reflect.TypeOf((*interface{})(nil)).Elem()
		}
	}
	ret := make([]reflect.Value, len(types))
	for i, typ := range types {
		if typ == nil {
			return nil, fmt.Errorf("decode argument %d: nil type", i)
		}
		v := reflect.New(typ)
		if i < len(d.args) {
			if err := json.Unmarshal(d.args[i], v.Interface()); err != nil {
				return nil, fmt.Errorf("decode argument %d: %w", i, err)
			}
		}
		ret[i] = v.Elem()
		if err := d.detachBuffer(ret[i], buffers); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func (d *Decoder) readHeader(r byteReader, h *Header) error {
	typ, err := r.ReadByte()
	if err != nil {
		return err
	}
	if typ < '0' || typ > '6' {
		return ErrInvalidPacketType
	}
	h.Type = Type(typ - '0')
	if h.Type == binaryEvent || h.Type == binaryAck {
		digits := false
		for {
			b, err := r.ReadByte()
			if err != nil {
				return errIllegalAttachments
			}
			if b == '-' {
				if !digits || d.bufferCount == 0 {
					return errIllegalAttachments
				}
				break
			}
			if b < '0' || b > '9' {
				return errIllegalAttachments
			}
			digits = true
			d.bufferCount = d.bufferCount*10 + uint64(b-'0')
			if d.bufferCount > maxAttachments {
				return errTooManyAttachments
			}
		}
	}
	b, err := r.ReadByte()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	if b == '/' {
		var namespace strings.Builder
		namespace.WriteByte(b)
		for {
			b, err = r.ReadByte()
			if err != nil && err != io.EOF {
				return err
			}
			if err == io.EOF || b == ',' {
				break
			}
			namespace.WriteByte(b)
		}
		h.Namespace, h.Query, _ = strings.Cut(namespace.String(), "?")
		if err == io.EOF {
			return nil
		}
	} else if err := r.UnreadByte(); err != nil {
		return err
	}
	h.ID, h.NeedAck, err = d.readUint64FromText(r)
	return err
}

func (d *Decoder) readUint64FromText(r byteReader) (uint64, bool, error) {
	var n uint64
	var read bool
	for {
		b, err := r.ReadByte()
		if err == io.EOF {
			return n, read, nil
		}
		if err != nil {
			return 0, false, err
		}
		if b < '0' || b > '9' {
			return n, read, r.UnreadByte()
		}
		digit := uint64(b - '0')
		if n > (math.MaxUint64-digit)/10 {
			return 0, false, fmt.Errorf("packet id: %w", strconv.ErrRange)
		}
		n = n*10 + digit
		read = true
	}
}

func (d *Decoder) validatePayload(typ Type, raw []byte, event *string) error {
	// The reference decoder validates a payload only when one is present.
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return errInvalidPayload
	}
	object := raw[0] == '{' || raw[0] == '[' || bytes.Equal(raw, []byte("null"))
	var array []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &array); err != nil {
			return errInvalidPayload
		}
	}
	switch typ {
	case Connect:
		if !object {
			return errInvalidPayload
		}
	case Disconnect:
		return errInvalidPayload
	case Error:
		if raw[0] != '"' && !object {
			return errInvalidPayload
		}
	case Event, binaryEvent:
		if len(array) == 0 {
			return errInvalidPayload
		}
		name := array[0]
		if name[0] == '"' {
			if err := json.Unmarshal(name, event); err != nil {
				return errInvalidPayload
			}
		} else {
			if name[0] != '-' && (name[0] < '0' || name[0] > '9') {
				return errInvalidPayload
			}
			number, err := strconv.ParseFloat(string(name), 64)
			if err != nil && !errors.Is(err, strconv.ErrRange) {
				return errInvalidPayload
			}
			*event = eventNumber(number)
		}
		array = array[1:]
	case Ack, binaryAck:
		if raw[0] != '[' {
			return errInvalidPayload
		}
	}
	if typ == binaryEvent || typ == binaryAck {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value interface{}
		if err := decoder.Decode(&value); err != nil {
			return errInvalidPayload
		}
		if err := d.validatePlaceholders(value); err != nil {
			return err
		}
	}
	if raw[0] == '[' && (typ == Event || typ == binaryEvent || typ == Ack || typ == binaryAck) {
		d.args = array
	} else {
		d.args = []json.RawMessage{raw}
	}
	return nil
}

// eventNumber uses JavaScript's decimal/exponent thresholds for numeric names.
func eventNumber(n float64) string {
	if math.IsInf(n, 1) {
		return "Infinity"
	}
	if math.IsInf(n, -1) {
		return "-Infinity"
	}
	if n == 0 {
		return "0"
	}
	format := byte('f')
	if math.Abs(n) >= 1e21 || math.Abs(n) < 1e-6 {
		format = 'e'
	}
	name := strconv.FormatFloat(n, format, -1, 64)
	name = strings.ReplaceAll(name, "e+0", "e+")
	return strings.ReplaceAll(name, "e-0", "e-")
}

func (d *Decoder) validatePlaceholders(value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		if placeholder, ok := v["_placeholder"].(bool); ok && placeholder {
			number, ok := v["num"].(json.Number)
			if !ok {
				return errIllegalAttachments
			}
			n, err := number.Float64()
			if err != nil || n < 0 || n != math.Trunc(n) || n >= float64(d.bufferCount) {
				return errIllegalAttachments
			}
			return nil
		}
		for _, child := range v {
			if err := d.validatePlaceholders(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range v {
			if err := d.validatePlaceholders(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Decoder) readBuffer(ft FrameType, r io.ReadCloser) ([]byte, error) {
	if ft != Binary {
		_ = r.Close()
		return nil, errInvalidBinaryBufferType
	}
	data, err := io.ReadAll(r)
	closeErr := r.Close()
	if err == nil {
		err = closeErr
	}
	return data, err
}

func (d *Decoder) detachBuffer(v reflect.Value, buffers []Buffer) error {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		if v.Kind() == reflect.Interface && v.CanSet() && len(buffers) > 0 {
			if object, ok := v.Interface().(map[string]interface{}); ok {
				if placeholder, _ := object["_placeholder"].(bool); placeholder {
					number, ok := object["num"].(float64)
					if !ok || number < 0 || number >= float64(len(buffers)) || number != math.Trunc(number) {
						return errIllegalAttachments
					}
					buffer := buffers[int(number)]
					v.Set(reflect.ValueOf(&buffer))
					return nil
				}
			}
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(Buffer{}) {
			if !v.CanAddr() || !v.Addr().CanInterface() {
				return errFailedBufferAddress
			}
			buffer := v.Addr().Interface().(*Buffer)
			if buffer.isBinary {
				if buffer.num >= uint64(len(buffers)) {
					return errIllegalAttachments
				}
				*buffer = buffers[buffer.num]
			}
			return nil
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if !field.IsExported() && !field.Anonymous {
				continue
			}
			if err := d.detachBuffer(v.Field(i), buffers); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			// Map values are not addressable, including a Buffer held by value.
			child := reflect.New(v.Type().Elem()).Elem()
			child.Set(v.MapIndex(key))
			if err := d.detachBuffer(child, buffers); err != nil {
				return err
			}
			v.SetMapIndex(key, child)
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := d.detachBuffer(v.Index(i), buffers); err != nil {
				return err
			}
		}
	}
	return nil
}
