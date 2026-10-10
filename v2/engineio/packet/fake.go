package packet

import (
	"bytes"
	"io"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
)

type fakeOneFrameDiscarder struct{}

func (d fakeOneFrameDiscarder) Write(p []byte) (int, error) {
	return len(p), nil
}

func (d fakeOneFrameDiscarder) Close() error {
	return nil
}

type FakeDiscardWriter struct{}

func (w *FakeDiscardWriter) NextWriter(fType frame.Type) (io.WriteCloser, error) {
	return fakeOneFrameDiscarder{}, nil
}

type fakeFrame struct {
	w    *fakeConnWriter
	typ  frame.Type
	data *bytes.Buffer
}

func newFakeFrame(w *fakeConnWriter, fType frame.Type) *fakeFrame {
	return &fakeFrame{
		w:    w,
		typ:  fType,
		data: bytes.NewBuffer(nil),
	}
}

func (w *fakeFrame) Write(p []byte) (int, error) {
	return w.data.Write(p)
}

func (w *fakeFrame) Read(p []byte) (int, error) {
	return w.data.Read(p)
}

func (w *fakeFrame) Close() error {
	if w.w == nil {
		return nil
	}
	w.w.Frames = append(w.w.Frames, Frame{
		FType: w.typ,
		Data:  w.data.Bytes(),
	})
	return nil
}

type fakeConnReader struct {
	frames []Frame
}

func NewFakeConnReader(frames []Frame) *fakeConnReader {
	return &fakeConnReader{
		frames: frames,
	}
}

func (r *fakeConnReader) NextReader() (frame.Type, io.ReadCloser, error) {
	if len(r.frames) == 0 {
		return frame.String, nil, io.EOF
	}
	f := r.frames[0]
	r.frames = r.frames[1:]
	return f.FType, io.NopCloser(bytes.NewReader(f.Data)), nil
}

type fakeOneFrameConst struct {
	b byte
}

func (c *fakeOneFrameConst) Read(p []byte) (int, error) {
	p[0] = c.b
	return 1, nil
}

type fakeConstReader struct {
	ft frame.Type
	r  *fakeOneFrameConst
}

func NewFakeConstReader() *fakeConstReader {
	return &fakeConstReader{
		ft: frame.String,
		r: &fakeOneFrameConst{
			b: MESSAGE.StringByte(),
		},
	}
}

func (r *fakeConstReader) NextReader() (frame.Type, io.ReadCloser, error) {
	ft := r.ft
	switch ft {
	case frame.Binary:
		r.ft = frame.String
		r.r.b = MESSAGE.StringByte()
	case frame.String:
		r.ft = frame.Binary
		r.r.b = MESSAGE.BinaryByte()
	}
	return ft, io.NopCloser(r.r), nil
}

type fakeConnWriter struct {
	Frames []Frame
}

func NewFakeConnWriter() *fakeConnWriter {
	return &fakeConnWriter{}
}

func (w *fakeConnWriter) NextWriter(fType frame.Type) (io.WriteCloser, error) {
	return newFakeFrame(w, fType), nil
}
