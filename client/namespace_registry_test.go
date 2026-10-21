package client

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceHandlerEvent(t *testing.T) {
	tests := []struct {
		name string

		events   []string
		handlers []interface{}

		event string
		args  []interface{}

		ok  bool
		ret []interface{}
	}{
		{
			name: "string handler",

			events: []string{"e", "n"},
			handlers: []interface{}{
				func(c Conn, str string) string {
					return "handled " + str
				},
				func(c Conn) {},
			},

			event: "e",
			args:  []interface{}{"str"},

			ok:  true,
			ret: []interface{}{"handled str"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			should := assert.New(t)
			must := require.New(t)

			h := newNamespaceHandler(test.name)
			for i, e := range test.events {
				h.OnEvent(e, test.handlers[i])
			}

			target := make([]reflect.Type, len(test.args))
			args := make([]reflect.Value, len(test.args))

			for i := range test.args {
				target[i] = reflect.TypeOf(test.args[i])
				args[i] = reflect.ValueOf(test.args[i])
			}

			types := h.Listeners(test.event)[0].handler.argTypes
			should.Equal(target, types)

			ret, err := h.DispatchEvent(&testConn{}, test.event, args...)
			must.NoError(err)

			res := make([]interface{}, len(ret))
			for i := range ret {
				res[i] = ret[i].Interface()
			}

			should.Equal(test.ret, res)
		})
	}
}
