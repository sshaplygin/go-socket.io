package parser

import (
	"bytes"
	"io"
)

type fakeOneFrameDiscarder struct{}

func (d fakeOneFrameDiscarder) Write(p []byte) (int, error) {
	return len(p), nil
}

func (d fakeOneFrameDiscarder) Close() error {
	return nil
}

type FakeDiscardWriter struct{}

func (w *FakeDiscardWriter) NextWriter(fType FrameType) (io.WriteCloser, error) {
	return fakeOneFrameDiscarder{}, nil
}

type fakeFrame struct {
	w    *fakeConnWriter
	typ  FrameType
	data *bytes.Buffer
}

func newFakeFrame(w *fakeConnWriter, fType FrameType) *fakeFrame {
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

func (r *fakeConnReader) NextReader() (FrameType, io.ReadCloser, error) {
	if len(r.frames) == 0 {
		return String, nil, io.EOF
	}
	f := r.frames[0]
	r.frames = r.frames[1:]
	return f.FType, io.NopCloser(bytes.NewReader(f.Data)), nil
}

type fakeOneFrameConst struct {
	b byte
}

func (c *fakeOneFrameConst) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = c.b
	return 1, nil
}

type fakeConstReader struct {
	ft FrameType
	r  *fakeOneFrameConst
}

func NewFakeConstReader() *fakeConstReader {
	return &fakeConstReader{
		ft: String,
		r: &fakeOneFrameConst{
			b: MESSAGE.StringByte(),
		},
	}
}

func (r *fakeConstReader) NextReader() (FrameType, io.ReadCloser, error) {
	ft := r.ft
	switch ft {
	case Binary:
		r.ft = String
		r.r.b = MESSAGE.BinaryByte()
	case String:
		r.ft = Binary
		r.r.b = MESSAGE.StringByte()
	}
	return ft, io.NopCloser(r.r), nil
}

type fakeConnWriter struct {
	Frames []Frame
}

func NewFakeConnWriter() *fakeConnWriter {
	return &fakeConnWriter{}
}

func (w *fakeConnWriter) NextWriter(fType FrameType) (io.WriteCloser, error) {
	return newFakeFrame(w, fType), nil
}

var _ FrameWriter = (*FakeDiscardWriter)(nil)
