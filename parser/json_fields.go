package parser

import (
	"encoding"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode"
)

var (
	jsonFieldsCache sync.Map // reflect.Type -> []jsonField, immutable after publication
	bufferType      = reflect.TypeOf(Buffer{})
	jsonMarshaler   = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// hasJSONMarshaler follows encoding/json's addressability rules. Buffer has its
// own wire representation; other marshalers own their contents, so inspecting
// their fields would transmit values that their marshaled output can hide.
func hasJSONMarshaler(v reflect.Value) bool {
	t := v.Type()
	if v.Kind() == reflect.Interface || t == bufferType || (t.Kind() == reflect.Pointer && t.Elem() == bufferType) {
		return false
	}
	if t.Implements(jsonMarshaler) || t.Implements(textMarshaler) {
		return true
	}
	return v.CanAddr() && (reflect.PointerTo(t).Implements(jsonMarshaler) || reflect.PointerTo(t).Implements(textMarshaler))
}

type jsonField struct {
	path     []int
	omitZero bool
}

// jsonFields selects the same struct fields as encoding/json: shallow fields
// dominate embedded fields, a tagged name wins at equal depth, and otherwise
// conflicting names disappear. Selection depends on types, including when an
// embedded pointer is nil. JSON encoding itself remains with encoding/json.
func jsonFields(t reflect.Type) []jsonField {
	if fields, ok := jsonFieldsCache.Load(t); ok {
		return fields.([]jsonField)
	}
	type scope struct {
		typ      reflect.Type
		path     []int
		multiple bool
	}
	type field struct {
		path      []int
		tagged    bool
		ambiguous bool
		omitZero  bool
	}
	selected := make(map[string]field)
	visited := make(map[reflect.Type]bool)
	level := []scope{{typ: t}}
	for len(level) > 0 {
		var next []scope
		queued := make(map[reflect.Type]int)
		for _, parent := range level {
			if visited[parent.typ] {
				continue
			}
			visited[parent.typ] = true
			for i := 0; i < parent.typ.NumField(); i++ {
				sf := parent.typ.Field(i)
				embedded := sf.Type
				if embedded.Kind() == reflect.Pointer {
					embedded = embedded.Elem()
				}
				if !sf.IsExported() && (!sf.Anonymous || embedded.Kind() != reflect.Struct) {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, options, _ := strings.Cut(tag, ",")
				if !validJSONFieldName(name) {
					name = ""
				}
				path := append(append([]int(nil), parent.path...), i)
				if sf.Anonymous && name == "" && embedded.Kind() == reflect.Struct {
					if index, ok := queued[embedded]; ok {
						next[index].multiple = true
					} else {
						queued[embedded] = len(next)
						next = append(next, scope{embedded, path, parent.multiple})
					}
					continue
				}
				tagged := name != ""
				if name == "" {
					name = sf.Name
				}
				old, found := selected[name]
				switch {
				case !found || len(path) < len(old.path) || (len(path) == len(old.path) && tagged && !old.tagged):
					selected[name] = field{path, tagged, parent.multiple, slices.Contains(strings.Split(options, ","), "omitzero")}
				case len(path) == len(old.path) && tagged == old.tagged:
					old.ambiguous = true
					selected[name] = old
				}
			}
		}
		level = next
	}
	var fields []jsonField
	for _, field := range selected {
		if !field.ambiguous {
			fields = append(fields, jsonField{field.path, field.omitZero})
		}
	}
	slices.SortFunc(fields, func(a, b jsonField) int { return slices.Compare(a.path, b.path) })
	cached, _ := jsonFieldsCache.LoadOrStore(t, fields)
	return cached.([]jsonField)
}

func validJSONFieldName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c) && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

func jsonFieldValue(v reflect.Value, path []int) reflect.Value {
	for _, index := range path {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		v = v.Field(index)
	}
	return v
}

// setJSONField copies embedded pointers before replacing a selected field.
// An unexported embedded pointer cannot be replaced through reflection.
func setJSONField(v reflect.Value, path []int, replacement reflect.Value) error {
	for i, index := range path {
		v = v.Field(index)
		if i == len(path)-1 {
			if !v.CanSet() {
				return errUnsupportedBuffer
			}
			v.Set(replacement)
			return nil
		}
		if v.Kind() == reflect.Pointer {
			if !v.CanSet() {
				return errUnsupportedBuffer
			}
			p := reflect.New(v.Type().Elem())
			p.Elem().Set(v.Elem())
			v.Set(p)
			v = p.Elem()
		}
	}
	return nil
}
