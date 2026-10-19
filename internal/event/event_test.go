package event

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type textReader struct{ text string }

func (r textReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	return parser.Text, io.NopCloser(strings.NewReader(r.text)), nil
}
func decoder(t *testing.T, packet string) *parser.Decoder {
	t.Helper()
	d := parser.NewDecoder(textReader{packet})
	var h parser.Header
	var name string
	if err := d.DecodeHeader(&h, &name); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPayloadSchema(t *testing.T) {
	for _, packet := range []string{`2["n",9007199254740993]`, `2["n",9223372036854775807]`} {
		p, err := Decode(decoder(t, packet), SchemaOf[int64]())
		if err != nil {
			t.Fatal(err)
		}
		got, err := Value[int64](p)
		if err != nil || got < 9007199254740993 || !p.Valid() || p.Interface() != got {
			t.Fatalf("value=%v err=%v", got, err)
		}
		if _, err := Value[int](p); !errors.Is(err, ErrInvalidPayload) {
			t.Fatal(err)
		}
	}
	for _, packet := range []string{`2["n"]`, `2["n",1,2]`, `2["n","wrong"]`} {
		if _, err := Decode(decoder(t, packet), SchemaOf[int]()); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("%s: %v", packet, err)
		}
	}
	p := ValueOf[interface{}](nil)
	if value, err := Value[interface{}](p); err != nil || value != nil {
		t.Fatalf("%v %v", value, err)
	}
	if _, err := Value[int](Payload{}); !errors.Is(err, ErrInvalidPayload) {
		t.Fatal(err)
	}
	if (Payload{}).Interface() != nil {
		t.Fatal("zero payload")
	}
	if _, err := Decode(decoder(t, `2["n",1]`), Schema{}); !errors.Is(err, ErrInvalidPayload) {
		t.Fatal(err)
	}
}

func TestAckEnvelope(t *testing.T) {
	p, err := DecodeAck(decoder(t, `30[null,9007199254740993]`), SchemaOf[int64]())
	got, valueErr := Value[int64](p)
	if err != nil || valueErr != nil || got != 9007199254740993 {
		t.Fatalf("%v %v %v", got, err, valueErr)
	}
	_, err = DecodeAck(decoder(t, `30[{"code":"denied","message":"no"}]`), SchemaOf[int]())
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Error() != "denied: no" {
		t.Fatal(err)
	}
	for _, packet := range []string{`30[]`, `30[null]`, `30[null,1,2]`, `30[false,1]`, `30[null,"wrong"]`, `30[{"code":"","message":"no"}]`, `30[{"code":"x"}]`, `30[{"code":"x","message":1}]`, `30[{"code":"x","message":"no","extra":1}]`} {
		if _, err := DecodeAck(decoder(t, packet), SchemaOf[int]()); !errors.Is(err, ErrInvalidAck) {
			t.Fatalf("%s: %v", packet, err)
		}
	}
	if _, err := DecodeAck(decoder(t, `30[null,1]`), Schema{}); !errors.Is(err, ErrInvalidAck) {
		t.Fatal(err)
	}
	if got := AckArgs(ValueOf(42), nil); len(got) != 2 || got[0] != nil || got[1] != 42 {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		err  error
		code string
	}{{errors.New("bad"), "handler_error"}, {remote, "denied"}, {&RemoteError{Message: "bad"}, "handler_error"}} {
		got := AckArgs(Payload{}, tc.err)
		if len(got) != 1 || got[0].(RemoteError).Code != tc.code {
			t.Fatal(got)
		}
	}
	if (*RemoteError)(nil).Error() != "" {
		t.Fatal("nil remote error")
	}
}

func TestHandlerInvocation(t *testing.T) {
	h := NewHandler(SchemaOf[int](), true, func(_ context.Context, _ Endpoint, p Payload) (Payload, error) { return p, nil })
	if !h.Valid() || !h.WantsAck() {
		t.Fatal("invalid handler")
	}
	p, err := h.Decode(decoder(t, `2["n",42]`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.Invoke(context.Background(), nil, p)
	if err != nil || result.Interface() != 42 {
		t.Fatalf("%v %v", result, err)
	}
	panicHandler := NewHandler(SchemaOf[int](), false, func(context.Context, Endpoint, Payload) (Payload, error) { panic("test panic") })
	_, err = panicHandler.Invoke(context.Background(), nil, p)
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code != "handler_panic" {
		t.Fatal(err)
	}
	if _, err := (Handler{}).Invoke(context.Background(), nil, p); !errors.Is(err, ErrInvalidPayload) {
		t.Fatal(err)
	}
}

func TestValidation(t *testing.T) {
	for _, name := range []string{"", "connect", "disconnect", "disconnecting", "error"} {
		if !errors.Is(ValidateName(name), ErrInvalidEventName) {
			t.Fatal(name)
		}
	}
	if ValidateName("message") != nil {
		t.Fatal("message is allowed")
	}
	for _, value := range []interface{}{nil, (*int)(nil), (func())(nil), ([]int)(nil), (map[string]int)(nil)} {
		if !IsNil(value) {
			t.Fatalf("not nil: %T", value)
		}
	}
	if IsNil(1) || IsNil(&struct{}{}) {
		t.Fatal("non-nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if Context(ctx) != ctx || Context(nil) == nil {
		t.Fatal("context")
	}
}
