package packet

import (
	"io"

	"github.com/sshaplygin/go-socket.io/logger"
)

// FrameWriter is the writer which supports framing.
type FrameWriter interface {
	NextWriter(typ FrameType) (io.WriteCloser, error)
}

type Encoder struct {
	w FrameWriter
}

func NewEncoder(w FrameWriter) *Encoder {
	return &Encoder{
		w: w,
	}
}

func (e *Encoder) NextWriter(ft FrameType, pt PacketType) (io.WriteCloser, error) {
	w, err := e.w.NextWriter(ft)
	if err != nil {
		return nil, err
	}

	var b [1]byte
	if ft == String {
		b[0] = pt.StringByte()
	} else {
		b[0] = pt.BinaryByte()
	}

	if _, err := w.Write(b[:]); err != nil {
		if closeErr := w.Close(); closeErr != nil {
			logger.Log.Debug("engineio: close writer failed", "err", closeErr)
		}

		return nil, err
	}

	return w, nil
}
