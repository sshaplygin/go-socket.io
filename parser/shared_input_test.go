package parser

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Arguments given to Emit or a broadcast are shared by the encoders of every connection that
// receives them and must stay read-only. This test encodes every fixture of `tests` (so a new
// fixture is covered without further work) and the shapes below from several goroutines on one
// shared value, and compares the value with a deep copy taken before. It needs -race to catch
// the writes themselves (make test-race, make test-stress).
const sharedWorkers, sharedRounds = 8, 25

type sharedShape struct {
	A *Buffer
	N int
	B []*Buffer
}

var sharedArgs = []struct {
	name string
	args []interface{}
}{
	{"struct", []interface{}{"e", &sharedShape{A: &Buffer{Data: []byte{1}}, N: 2, B: []*Buffer{{Data: []byte{3}}, {Data: []byte{4}}}}}},
	// one Buffer per map: map order, hence the numbering, differs between encodes
	{"map", []interface{}{"e", map[string]interface{}{"a": &Buffer{Data: []byte{1}}, "b": 2}, map[string][]*Buffer{"c": {{Data: []byte{3}}}}}},
	{"slice and array", []interface{}{"e", []*Buffer{{Data: []byte{1}}}, [2]*Buffer{{Data: []byte{2}}, {Data: []byte{3}}}}},
}

// deepCopy copies v. Set on a struct copies its unexported fields (Buffer's) with it.
func deepCopy(v reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			c := deepCopy(v.Elem())
			if v.Kind() == reflect.Pointer {
				out.Set(reflect.New(c.Type()))
				out.Elem().Set(c)
			} else {
				out.Set(c)
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && !v.IsNil() {
			out.Set(reflect.MakeSlice(v.Type(), v.Len(), v.Len()))
		}
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}
	case reflect.Map:
		if !v.IsNil() {
			out.Set(reflect.MakeMapWithSize(v.Type(), v.Len()))
			for it := v.MapRange(); it.Next(); {
				out.SetMapIndex(it.Key(), deepCopy(it.Value()))
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if out.Field(i).CanSet() {
				out.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
	}
	return out
}

// encodeShared encodes args from sharedWorkers goroutines and checks that every frame equals
// the frames of one sequential encode, then that args is unchanged.
func encodeShared(t *testing.T, h Header, args []interface{}) {
	t.Helper()
	before := deepCopy(reflect.ValueOf(args)).Interface()
	frames := func(w *fakeWriter) string { return fmt.Sprintf("%v %q", w.types, w.data) }

	ref := fakeWriter{}
	require.NoError(t, NewEncoder(&ref).Encode(h, args))

	var wg sync.WaitGroup
	for w := 0; w < sharedWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < sharedRounds; r++ {
				fw := fakeWriter{}
				if err := NewEncoder(&fw).Encode(h, args); err != nil || frames(&fw) != frames(&ref) {
					t.Errorf("frames %s (err %v), want %s", frames(&fw), err, frames(&ref))
					return
				}
			}
		}()
	}
	wg.Wait()
	require.Equal(t, before, interface{}(args), "the encoder wrote to its shared input")
}

func TestEncodeSharedArgs(t *testing.T) {
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			args := test.Var
			if test.Header.Type == Event {
				args = append([]interface{}{test.Event}, test.Var...)
			}
			encodeShared(t, test.Header, args)
		})
	}
	for _, test := range sharedArgs {
		t.Run(test.name, func(t *testing.T) {
			encodeShared(t, Header{Type: Event}, test.args)
		})
	}
}
