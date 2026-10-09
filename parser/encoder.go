package parser

import (
	"bufio"
	"encoding/json"
	"io"
	"reflect"

	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/logger"
)

type FrameWriter interface {
	NextWriter(ft session.FrameType) (io.WriteCloser, error)
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
	var w io.WriteCloser
	w, err = e.w.NextWriter(session.TEXT)
	if err != nil {
		logger.Log.Debug("socketio: next writer failed", "err", err)

		return
	}

	var buffers [][]byte
	buffers, err = e.writePacket(w, h, args)
	if err != nil {
		logger.Log.Debug("socketio: write packet failed", "err", err)

		return
	}

	for _, b := range buffers {
		w, err = e.w.NextWriter(session.BINARY)
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
		if h.ID != 0 || args != nil {
			if err := bw.WriteByte(','); err != nil {
				return nil, err
			}
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

// attachBuffer returns the data of every Buffer reachable from v, in the order of their
// placeholder numbers. It never writes to v.
func (e *Encoder) attachBuffer(v reflect.Value, index *uint64) ([][]byte, error) {
	_, data, err := e.numberBuffers(v, index)
	return data, err
}

// numberBuffers walks v and returns the data of its Buffers together with a replacement of
// v in which every Buffer is a copy that carries its placeholder number. The values the
// caller passed to Emit or Broadcast are shared between connections, so they are
// read-only here: containers that hold a Buffer are copied, never changed. The returned
// Value is invalid when v holds no Buffer.
func (e *Encoder) numberBuffers(v reflect.Value, index *uint64) (reflect.Value, [][]byte, error) {
	var (
		repl reflect.Value
		data [][]byte
	)
	// set stores the replacement of the child at position i into a private copy of v.
	set := func(child reflect.Value, i int, key reflect.Value) error {
		if !repl.IsValid() {
			if !v.CanInterface() {
				return errFailedBufferAddress
			}
			switch v.Kind() {
			case reflect.Slice:
				repl = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
				reflect.Copy(repl, v)
			case reflect.Map:
				repl = reflect.MakeMapWithSize(v.Type(), v.Len())
				for it := v.MapRange(); it.Next(); {
					repl.SetMapIndex(it.Key(), it.Value())
				}
			default:
				repl = reflect.New(v.Type()).Elem()
				repl.Set(v)
			}
		}
		switch v.Kind() {
		case reflect.Map:
			repl.SetMapIndex(key, child)
		case reflect.Struct:
			if !repl.Field(i).CanSet() {
				return errFailedBufferAddress
			}
			repl.Field(i).Set(child)
		default:
			repl.Index(i).Set(child)
		}
		return nil
	}
	walk := func(child reflect.Value, i int, key reflect.Value) error {
		c, b, err := e.numberBuffers(child, index)
		if err != nil {
			return err
		}
		data = append(data, b...)
		if !c.IsValid() {
			return nil
		}
		return set(c, i, key)
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return reflect.Value{}, nil, nil
		}
		c, b, err := e.numberBuffers(v.Elem(), index)
		if err != nil || !c.IsValid() {
			return reflect.Value{}, b, err
		}
		if v.Kind() == reflect.Pointer {
			p := reflect.New(c.Type())
			p.Elem().Set(c)
			c = p
		}
		return c, b, nil

	case reflect.Struct:
		if v.Type().Name() == bufferTypeName {
			if !v.CanAddr() || !v.CanInterface() {
				return reflect.Value{}, nil, errFailedBufferAddress
			}
			src := v.Addr().Interface().(*Buffer)
			numbered := Buffer{num: *index, isBinary: true, Data: src.Data}
			*index++
			return reflect.ValueOf(numbered), [][]byte{src.Data}, nil
		}
		for i := 0; i < v.NumField(); i++ {
			if err := walk(v.Field(i), i, reflect.Value{}); err != nil {
				return reflect.Value{}, nil, err
			}
		}

	case reflect.Array, reflect.Slice:
		switch v.Type().Elem().Kind() {
		case reflect.Struct, reflect.Pointer, reflect.Interface, reflect.Array, reflect.Slice, reflect.Map:
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i), i, reflect.Value{}); err != nil {
					return reflect.Value{}, nil, err
				}
			}
		}

	case reflect.Map:
		for _, key := range v.MapKeys() {
			if err := walk(v.MapIndex(key), 0, key); err != nil {
				return reflect.Value{}, nil, err
			}
		}
	}

	return repl, data, nil
}

func (e *Encoder) writeBuffer(w io.WriteCloser, buffer []byte) error {
	defer func() {
		if closeErr := w.Close(); closeErr != nil {
			logger.Log.Debug("socketio: close writer failed", "err", closeErr)
		}
	}()

	_, err := w.Write(buffer)
	return err
}
