package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/sshaplygin/go-socket.io/logger"
)

type FrameWriter interface {
	NextWriter(ft FrameType) (io.WriteCloser, error)
}

type Encoder struct {
	w FrameWriter
}

func NewEncoder(w FrameWriter) *Encoder {
	return &Encoder{
		w: w,
	}
}

func (e *Encoder) Encode(h Header, args ...interface{}) (err error) {
	packet, err := Prepare(h, args...)
	if err != nil {
		return err
	}
	return e.EncodePrepared(packet)
}

// PreparedPacket owns an encoded packet and its binary attachments. Its contents
// are immutable and may be written to multiple connections concurrently.
type PreparedPacket struct {
	text    []byte
	buffers [][]byte
}

// Prepare snapshots the JSON payload and binary attachments before queueing a packet.
func Prepare(h Header, args ...interface{}) (*PreparedPacket, error) {
	var data bytes.Buffer
	buffers, err := new(Encoder).writePacket(bufferCloser{&data}, h, args)
	if err != nil {
		return nil, err
	}
	packet := &PreparedPacket{text: data.Bytes(), buffers: make([][]byte, len(buffers))}
	for i, b := range buffers {
		packet.buffers[i] = bytes.Clone(b)
	}
	return packet, nil
}

// EncodePrepared writes a snapshot previously returned by Prepare.
func (e *Encoder) EncodePrepared(packet *PreparedPacket) (err error) {
	w, err := e.w.NextWriter(Text)
	if err != nil {
		logger.Log.Debug("socketio: next writer failed", "err", err)
		return err
	}
	if err = e.writeBuffer(w, packet.text); err != nil {
		return err
	}

	for _, b := range packet.buffers {
		w, err = e.w.NextWriter(Binary)
		if err != nil {
			logger.Log.Debug("socketio: next writer failed", "err", err)

			return
		}

		err = e.writeBuffer(w, b)
		if err != nil {
			logger.Log.Debug("socketio: write buffer failed", "err", err)

			return
		}
	}

	return
}

type bufferCloser struct{ *bytes.Buffer }

func (bufferCloser) Close() error { return nil }

type byteWriter interface {
	io.Writer
	WriteByte(byte) error
}

type flusher interface {
	Flush() error
}

func (e *Encoder) writePacket(w io.WriteCloser, h Header, args []interface{}) ([][]byte, error) {
	defer func() {
		if err := w.Close(); err != nil {
			logger.Log.Debug("socketio: close writer failed", "err", err)
		}
	}()

	bw, ok := w.(byteWriter)
	if !ok {
		bw = bufio.NewWriter(w)
	}

	max := uint64(0)
	numbered, buffers, err := e.numberBuffers(reflect.ValueOf(args), &max)
	if err != nil {
		return nil, err
	}
	if numbered.IsValid() {
		args = numbered.Interface().([]interface{})
	}

	if len(buffers) > 0 && (h.Type == Event || h.Type == Ack) {
		h.Type += 3
	}

	if err := bw.WriteByte(byte(h.Type + '0')); err != nil {
		return nil, err
	}

	if h.Type == binaryAck || h.Type == binaryEvent {
		if err := e.writeUint64(bw, max); err != nil {
			return nil, err
		}
		if err := bw.WriteByte('-'); err != nil {
			return nil, err
		}
	}

	if h.Namespace != "" {
		if _, err := bw.Write([]byte(h.Namespace)); err != nil {
			return nil, err
		}
		if err := bw.WriteByte(','); err != nil {
			return nil, err
		}
	}

	if h.NeedAck {
		if err := e.writeUint64(bw, h.ID); err != nil {
			return nil, err
		}
	}

	if len(args) > 0 {
		if err := json.NewEncoder(bw).Encode(args[0]); err != nil {
			return nil, err
		}
	}

	if f, ok := bw.(flusher); ok {
		if err := f.Flush(); err != nil {
			return nil, err
		}
	}

	return buffers, nil
}

func (e *Encoder) writeUint64(w byteWriter, i uint64) error {
	base := uint64(1)
	for i/base >= 10 {
		base *= 10
	}
	for base > 0 {
		p := i / base
		if err := w.WriteByte(byte(p) + '0'); err != nil {
			return err
		}
		i -= p * base
		base /= 10
	}
	return nil
}

// attachBuffer returns the data of every Buffer in v in placeholder order; it never writes to v.
func (e *Encoder) attachBuffer(v reflect.Value, index *uint64) ([][]byte, error) {
	_, data, err := e.numberBuffers(v, index)
	return data, err
}

// numbering is the state of numberBuffers for the value v: the data found so far and repl, the
// private copy of v that numbered children are stored into.
type numbering struct {
	walker *bufferNumberer
	index  *uint64
	v      reflect.Value
	repl   reflect.Value
	data   [][]byte
}

// own makes repl on first use.
func (n *numbering) own() (reflect.Value, error) {
	if n.repl.IsValid() {
		return n.repl, nil
	}
	if !n.v.CanInterface() {
		return reflect.Value{}, errUnsupportedBuffer
	}
	n.repl = reflect.New(n.v.Type()).Elem()
	n.repl.Set(n.v)
	switch n.v.Kind() {
	case reflect.Slice:
		n.repl.Set(reflect.MakeSlice(n.v.Type(), n.v.Len(), n.v.Len()))
		reflect.Copy(n.repl, n.v)
	case reflect.Map:
		n.repl.Set(reflect.MakeMapWithSize(n.v.Type(), n.v.Len()))
		for it := n.v.MapRange(); it.Next(); {
			n.repl.SetMapIndex(it.Key(), it.Value())
		}
	}
	return n.repl, nil
}

// child numbers the element c at i (or key) of a slice, array or map and stores it into repl.
func (n *numbering) child(c reflect.Value, i int, key reflect.Value) error {
	c, b, err := n.walker.numberBuffers(c, n.index)
	n.data = append(n.data, b...)
	if err != nil || !c.IsValid() {
		return err
	}
	r, err := n.own()
	if err != nil {
		return err
	}
	if n.v.Kind() == reflect.Map {
		r.SetMapIndex(key, c)
	} else {
		r.Index(i).Set(c)
	}
	return nil
}

// fields numbers the Buffers in the fields of s, the struct v or an embedded unexported struct of
// it at path, and stores them into repl; reflect refuses to set the embedded field itself.
func (n *numbering) fields(s reflect.Value, path []int) error {
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		if f.Kind() == reflect.Struct {
			if sf := s.Type().Field(i); sf.Anonymous && !sf.IsExported() {
				if err := n.fields(f, append(path[:len(path):len(path)], i)); err != nil {
					return err
				}
				continue
			}
		}
		if s.Type().Field(i).Tag.Get("json") == "-" {
			continue
		}
		c, b, err := n.walker.numberBuffers(f, n.index)
		n.data = append(n.data, b...)
		if err != nil {
			return err
		}
		if c.IsValid() {
			r, err := n.own()
			if err != nil {
				return err
			}
			r.FieldByIndex(append(path[:len(path):len(path)], i)).Set(c)
		}
	}
	return nil
}

// numberBuffers returns the data of the Buffers in v and a replacement of v in which each Buffer
// is a numbered copy. Values given to Emit or Broadcast are shared between connections: the
// containers that hold a Buffer are copied, never changed. The Value is invalid if v has none.
func (e *Encoder) numberBuffers(v reflect.Value, index *uint64) (reflect.Value, [][]byte, error) {
	return (&bufferNumberer{active: make(map[bufferVisit]bool)}).numberBuffers(v, index)
}

type bufferVisit struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

type bufferNumberer struct {
	active map[bufferVisit]bool
	depth  int
}

func (w *bufferNumberer) numberBuffers(v reflect.Value, index *uint64) (reflect.Value, [][]byte, error) {
	if !v.IsValid() {
		return reflect.Value{}, nil, nil
	}
	w.depth++
	defer func() { w.depth-- }()
	if w.depth > 1000 {
		return reflect.Value{}, nil, &json.UnsupportedValueError{Value: v, Str: "value exceeds maximum nesting depth"}
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		if !v.IsNil() {
			visit := bufferVisit{typ: v.Type(), ptr: v.Pointer()}
			if v.Kind() == reflect.Slice {
				visit.length = v.Len()
			}
			if w.active[visit] {
				return reflect.Value{}, nil, &json.UnsupportedValueError{Value: v, Str: "encountered a cycle"}
			}
			w.active[visit] = true
			defer delete(w.active, visit)
		}
	}
	n := numbering{walker: w, index: index, v: v}
	var err error
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return reflect.Value{}, nil, nil
		}
		c, b, err := w.numberBuffers(v.Elem(), index)
		if err == nil && c.IsValid() && v.Kind() == reflect.Pointer {
			p := reflect.New(c.Type())
			p.Elem().Set(c)
			c = p
		}
		return c, b, err

	case reflect.Struct:
		if v.Type() == reflect.TypeOf(Buffer{}) {
			if !v.CanInterface() {
				return reflect.Value{}, nil, errUnsupportedBuffer
			}
			src := v.Interface().(Buffer)
			numbered := Buffer{num: *index, isBinary: true, Data: src.Data}
			*index++
			return reflect.ValueOf(numbered), [][]byte{src.Data}, nil
		}
		err = n.fields(v, nil)

	case reflect.Array, reflect.Slice:
		switch v.Type().Elem().Kind() {
		case reflect.Struct, reflect.Pointer, reflect.Interface, reflect.Array, reflect.Slice, reflect.Map:
			for i := 0; i < v.Len() && err == nil; i++ {
				err = n.child(v.Index(i), i, reflect.Value{})
			}
		}

	case reflect.Map:
		for _, key := range v.MapKeys() {
			if err = n.child(v.MapIndex(key), 0, key); err != nil {
				break
			}
		}
	}
	if err != nil {
		return reflect.Value{}, nil, err
	}
	return n.repl, n.data, nil
}

func (e *Encoder) writeBuffer(w io.WriteCloser, buffer []byte) (err error) {
	defer func() {
		if closeErr := w.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
	}()

	_, err = w.Write(buffer)
	return err
}
