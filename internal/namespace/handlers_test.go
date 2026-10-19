package namespace

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

func TestNewEventFunc(t *testing.T) {
	tests := []struct {
		f        interface{}
		ok       bool
		argTypes []interface{}
	}{
		{1, false, []interface{}{}},
		{func() {}, false, []interface{}{}},
		{func(int) {}, false, []interface{}{}},
		{func() error { return nil }, false, []interface{}{}},

		{func(Conn) {}, true, []interface{}{}},
		{func(Conn, int) {}, true, []interface{}{1}},
		{func(Conn, int) error { return nil }, true, []interface{}{1}},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%#v", test.argTypes), func(t *testing.T) {
			should := assert.New(t)
			must := require.New(t)

			defer func() {
				r := recover()
				must.Equal(test.ok, r == nil)
			}()

			h := NewEventFunc(test.f)
			must.Equal(len(test.argTypes), len(h.argTypes))
			for i := range h.argTypes {
				should.Equal(reflect.TypeOf(test.argTypes[i]), h.argTypes[i])
			}
		})
	}
}

func TestNewAckFunc(t *testing.T) {
	tests := []struct {
		f        interface{}
		ok       bool
		argTypes []interface{}
	}{
		{1, false, []interface{}{}},

		{func() {}, true, []interface{}{}},
		{func(int) {}, true, []interface{}{1}},
		{func(int) error { return nil }, true, []interface{}{1}},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%#v", test.argTypes), func(t *testing.T) {
			should := assert.New(t)
			must := require.New(t)

			defer func() {
				r := recover()
				must.Equal(test.ok, r == nil)
			}()

			h := NewAckFunc(test.f)
			must.Equal(len(test.argTypes), len(h.argTypes))

			for i := range h.argTypes {
				should.Equal(reflect.TypeOf(test.argTypes[i]), h.argTypes[i])
			}
		})
	}
}

func TestHandlerCall(t *testing.T) {
	tests := []struct {
		f    interface{}
		args []interface{}
		ok   bool
		rets []interface{}
	}{
		{func() {}, []interface{}{1}, false, nil},

		{func() {}, nil, true, nil},
		{func(int) {}, []interface{}{1}, true, nil},
		{func() int { return 1 }, nil, true, []interface{}{1}},
		{func(int) int { return 1 }, []interface{}{1}, true, []interface{}{1}},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%#v", test.f), func(t *testing.T) {
			should := assert.New(t)
			must := require.New(t)

			h := NewAckFunc(test.f)

			args := make([]reflect.Value, len(test.args))
			for i := range args {
				args[i] = reflect.ValueOf(test.args[i])
			}

			retV, err := h.Call(args)
			must.Equal(test.ok, err == nil)

			if len(retV) == len(test.rets) && len(test.rets) == 0 {
				return
			}

			rets := make([]interface{}, len(retV))
			for i := range rets {
				rets[i] = retV[i].Interface()
			}

			should.Equal(test.rets, rets)
		})
	}
}

// Covers G18
func TestRegressionG18ConnectPanicIsContained(t *testing.T) {
	h := NewHandler()
	h.OnConnect(func(Conn) error { panic("application panic") })
	require.NotPanics(t, func() { require.Error(t, h.Connect(nil)) })
}

func TestConnectMiddlewarePanicAndNil(t *testing.T) {
	h := NewHandler()
	h.Use(nil)
	require.NoError(t, h.Connect(nil))
	h.Use(func(Conn) error { panic("middleware panic") })
	require.ErrorContains(t, h.Connect(nil), "middleware panic")
}

// Covers G20
func TestRegressionG20VariadicHandlerArguments(t *testing.T) {
	called := false
	h := NewEventFunc(func(_ Conn, args ...string) { called = true })
	_, err := h.Call([]reflect.Value{reflect.Zero(reflect.TypeOf((*Conn)(nil)).Elem()), reflect.ValueOf([]string{"a", "b"})})
	require.NoError(t, err, "registered argument type is []string, but dispatch cannot call the variadic handler with it")
	require.True(t, called)
}

// Covers G20
func TestListenerVariadicConventionsMustMatch(t *testing.T) {
	h := NewHandler()
	h.AddListener("event", func(Conn, ...string) {}, false)
	require.Panics(t, func() { h.AddListener("event", func(Conn, []string) {}, false) })
}
