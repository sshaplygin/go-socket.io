package parser

import (
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

// initialPayloadCapacity is the payload space Prepare allocates together with
// the reserved header. It covers short payloads such as an event name with a few
// scalar arguments in one allocation; larger payloads grow the buffer as usual.
const initialPayloadCapacity = 64

// Prepare snapshots the JSON payload and binary attachments before queueing a packet.
// It rejects encoded packets containing more than 10 binary attachments.
func Prepare(h Header, args ...interface{}) (*PreparedPacket, error) {
	// Reserve the largest possible header before encoding, then move its start
	// forward when the actual attachment count needs fewer bytes. The payload
	// stays in its final backing array instead of being copied into another buffer.
	maxHeader := preparedHeader(h, maxAttachments)
	reserved := len(maxHeader)
	payload := new(payloadEncoder)
	payload.Grow(reserved + initialPayloadCapacity)
	payload.Write(maxHeader) // placeholder bytes, overwritten by the actual header
	if len(args) > 0 {
		// Only the first argument is the packet payload.
		if err := payload.value(reflect.ValueOf(args[0]), false); err != nil {
			return nil, err
		}
		payload.WriteByte('\n')
	}
	header := preparedHeader(h, len(payload.buffers))
	text := payload.Bytes()[reserved-len(header):]
	copy(text, header)
	return &PreparedPacket{text: text, payload: text[len(header):], buffers: payload.buffers}, nil
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
