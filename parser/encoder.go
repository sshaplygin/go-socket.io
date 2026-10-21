package parser

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strconv"

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
	payload []byte
	text    []byte
	buffers [][]byte
}

// Prepare snapshots the JSON payload and binary attachments before queueing a packet.
// It rejects encoded packets containing more than 10 binary attachments.
func Prepare(h Header, args ...interface{}) (*PreparedPacket, error) {
	var data bytes.Buffer
	buffers, err := new(Encoder).writePacket(bufferCloser{&data}, h, args)
	if err != nil {
		return nil, err
	}
	packet := &PreparedPacket{text: data.Bytes(), buffers: buffers}
	packet.payload = packet.text[len(preparedHeader(h, len(buffers))):]
	return packet, nil
}

// WithHeader returns the same immutable payload with a new packet header. It
// allows a queue to allocate an acknowledgement ID only after admission.
func (p *PreparedPacket) WithHeader(h Header) *PreparedPacket {
	text := preparedHeader(h, len(p.buffers))
	text = append(text, p.payload...)
	return &PreparedPacket{text: text, payload: p.payload, buffers: p.buffers}
}

func preparedHeader(h Header, attachments int) []byte {
	if attachments > 0 && (h.Type == Event || h.Type == Ack) {
		h.Type += 3
	}
	text := []byte{byte(h.Type + '0')}
	if h.Type == binaryAck || h.Type == binaryEvent {
		text = strconv.AppendInt(text, int64(attachments), 10)
		text = append(text, '-')
	}
	if h.Namespace != "" {
		text = append(text, h.Namespace...)
		text = append(text, ',')
	}
	if h.NeedAck {
		text = strconv.AppendUint(text, h.ID, 10)
	}
	return text
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

	payload := &payloadEncoder{active: make(map[jsonVisit]bool)}
	if len(args) > 0 {
		// Only the first argument is the packet payload.
		if err := payload.value(reflect.ValueOf(args[0]), false); err != nil {
			return nil, err
		}
		payload.WriteByte('\n')
	}
	buffers := payload.buffers
	max := uint64(len(buffers))
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

	if _, err := bw.Write(payload.Bytes()); err != nil {
		return nil, err
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
