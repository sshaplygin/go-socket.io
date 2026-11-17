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
	// A typed nil is success: its Error method would dereference nil.
	if got := AckArgs(ValueOf(42), (*derefError)(nil)); len(got) != 2 || got[0] != nil || got[1] != 42 {
		t.Fatal(got)
	}
}

type derefError struct{ msg string }

func (e *derefError) Error() string { return e.msg }

type sliceError []string

func (e sliceError) Error() string { return strings.Join(e, "; ") }

func TestNilError(t *testing.T) {
	for _, err := range []error{nil, (*derefError)(nil), (*RemoteError)(nil)} {
		if got := NilError(err); got != nil {
			t.Fatalf("NilError(%#v) = %v", err, got)
		}
	}
	if got := NilError(sliceError(nil)); got != nil {
		t.Fatalf("nil slice error %v", got)
	}
	plain := errors.New("bad")
	if got := NilError(plain); got != plain {
		t.Fatal(got)
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
	var panicked *PanicError
	if !errors.As(err, &panicked) || panicked.Value != "test panic" || !strings.Contains(string(panicked.Stack), "event_test.go") {
		t.Fatal(err)
	}
	if got := AckArgs(Payload{}, err); len(got) != 1 || got[0] != (RemoteError{Code: "handler_panic", Message: PanicMessage}) {
		t.Fatalf("panic ACK %v", got)
	}
	if _, err := (Handler{}).Invoke(context.Background(), nil, p); !errors.Is(err, ErrInvalidPayload) {
		t.Fatal(err)
	}
	typedNil := NewHandler(SchemaOf[int](), true, func(_ context.Context, _ Endpoint, p Payload) (Payload, error) {
		return p, (*derefError)(nil)
	})
	if result, err := typedNil.Invoke(context.Background(), nil, p); err != nil || result.Interface() != 42 {
		t.Fatalf("typed nil error: %v %v", result, err)
	}
}

//nolint:staticcheck // Verify the explicit nil-context fallback.
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

type panickingError struct{}

func (panickingError) Error() string { panic("error method boom") }

type panickingUnwrap struct{}

func (panickingUnwrap) Error() string { return "unwrap" }
func (panickingUnwrap) Unwrap() error { panic("unwrap boom") }

func TestReturnedRecoversErrorMethodPanics(t *testing.T) {
	plain := errors.New("plain")
	if got := Returned(plain); got != plain {
		t.Fatalf("Returned(plain) = %v", got)
	}
	if got := Returned((*derefError)(nil)); got != nil {
		t.Fatalf("Returned(typed nil) = %v", got)
	}
	for value, err := range map[string]error{"error method boom": panickingError{}, "unwrap boom": panickingUnwrap{}} {
		var panicked *PanicError
		got := Returned(err)
		if !errors.As(got, &panicked) || panicked.Value != value || len(panicked.Stack) == 0 {
			t.Fatalf("Returned(%T) = %v", err, got)
		}
		if got.Error() != "error method: panic: "+value {
			t.Fatal(got.Error())
		}
		// The encoders do not crash on the unchecked value either.
		if object := AckError(err); object != (RemoteError{Code: "handler_panic", Message: PanicMessage}) {
			t.Fatal(object)
		}
		if args := AckArgs(Payload{}, err); len(args) != 1 || args[0] != (RemoteError{Code: "handler_panic", Message: PanicMessage}) {
			t.Fatal(args)
		}
	}
	if got := WireMessage(panickingError{}); got != PanicMessage {
		t.Fatal(got)
	}
	h := NewHandler(SchemaOf[int](), true, func(context.Context, Endpoint, Payload) (Payload, error) {
		return Payload{}, panickingError{}
	})
	if _, err := h.Invoke(context.Background(), nil, ValueOf(1)); !IsPanic(err) {
		t.Fatalf("Invoke = %v", err)
	}
}
