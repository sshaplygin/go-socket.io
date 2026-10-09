package parser

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Arguments given to Emit or a broadcast are shared by the encoders of every connection that
// receives them, and a decoder input may be read by several decoders. Both must be read-only.
// These tests run every fixture of `tests` (so a new fixture is covered without further work)
// plus the argument shapes below from several goroutines on one shared value, and compare the
// value with a deep copy taken before. Run them with -race (make test-race, make test-stress).
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
	// one Buffer per map: the order of map keys, hence the numbering, differs between encodes
	{"map", []interface{}{"e", map[string]interface{}{"a": &Buffer{Data: []byte{1}}, "b": 2}, map[string][]*Buffer{"c": {{Data: []byte{3}}}}}},
	{"slice and array", []interface{}{"e", []*Buffer{{Data: []byte{1}}}, [2]*Buffer{{Data: []byte{2}}, {Data: []byte{3}}}}},
	{"same twice", []interface{}{"e", sharedBuffer, sharedBuffer, []interface{}{sharedBuffer}}},
}

var sharedBuffer = &Buffer{Data: []byte{9, 9}}

// deepCopy copies v including the unexported fields of Buffer, which reflect cannot set one by one.
func deepCopy(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	out := reflect.New(v.Type()).Elem()
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			p := reflect.New(v.Type().Elem())
			p.Elem().Set(deepCopy(v.Elem()))
			out.Set(p)
		}
	case reflect.Interface:
		if !v.IsNil() {
			out.Set(deepCopy(v.Elem()))
		}
	case reflect.Slice:
		if !v.IsNil() {
			out.Set(reflect.MakeSlice(v.Type(), v.Len(), v.Len()))
			for i := 0; i < v.Len(); i++ {
				out.Index(i).Set(deepCopy(v.Index(i)))
			}
		}
	case reflect.Array:
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
		out.Set(v) // unexported fields included
		for i := 0; i < v.NumField(); i++ {
			if out.Field(i).CanSet() {
				out.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
	default:
		out.Set(v)
	}
	return out
}

func requireUnchanged(t *testing.T, before, after interface{}) {
	t.Helper()
	require.True(t, reflect.DeepEqual(before, after), "the encoder or decoder wrote to its shared input:\nbefore %#v\nafter  %#v", before, after)
}

// encodeShared encodes args from sharedWorkers goroutines and checks that every frame equals
// the frames of one sequential encode, then that args is unchanged.
func encodeShared(t *testing.T, h Header, args []interface{}) {
	t.Helper()
	before := deepCopy(reflect.ValueOf(args)).Interface()

	ref := fakeWriter{}
	require.NoError(t, NewEncoder(&ref).Encode(h, args))
	requireUnchanged(t, before, args)

	var wg sync.WaitGroup
	errs := make(chan error, sharedWorkers)
	for w := 0; w < sharedWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < sharedRounds; r++ {
				fw := fakeWriter{}
				if err := NewEncoder(&fw).Encode(h, args); err != nil {
					errs <- err
					return
				}
				if !reflect.DeepEqual(fw.types, ref.types) || len(fw.data) != len(ref.data) {
					errs <- fmt.Errorf("frame types %v, want %v", fw.types, ref.types)
					return
				}
				for i := range fw.data {
					if !bytes.Equal(fw.data[i].Bytes(), ref.data[i].Bytes()) {
						errs <- fmt.Errorf("frame %d is %q, want %q", i, fw.data[i], ref.data[i])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	requireUnchanged(t, before, args)
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

func TestDecodeSharedFrames(t *testing.T) {
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			before := deepCopy(reflect.ValueOf(test.Data)).Interface()

			var wg sync.WaitGroup
			for w := 0; w < sharedWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for r := 0; r < sharedRounds; r++ {
						d := NewDecoder(&fakeReader{data: test.Data})
						var h Header
						var event string
						if d.DecodeHeader(&h, &event) == nil {
							types := make([]reflect.Type, len(test.Var))
							for i := range types {
								types[i] = reflect.TypeOf(test.Var[i])
							}
							_, _ = d.DecodeArgs(types)
						}
						_ = d.DiscardLast()
						_ = d.Close()
					}
				}()
			}
			wg.Wait()
			requireUnchanged(t, before, test.Data)
		})
	}
}
