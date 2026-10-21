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

type jsonField struct {
	path      []int
	name      []byte
	omitEmpty bool
	omitZero  bool
	quoted    bool
}

// jsonFields selects the same struct fields as encoding/json: shallow fields
// dominate embedded fields, a tagged name wins at equal depth, and otherwise
// conflicting names disappear. Selection depends on types, including when an
// embedded pointer is nil. Cached paths and options are immutable.
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
		omitEmpty bool
		quoted    bool
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
						next = append(next, scope{embedded, path, false})
					}
					continue
				}
				tagged := name != ""
				if name == "" {
					name = sf.Name
				}
				optionsList := strings.Split(options, ",")
				quoted := false
				if slices.Contains(optionsList, "string") {
					quotedType := sf.Type
					if (quoteNamedJSONPointers || quotedType.Name() == "") && quotedType.Kind() == reflect.Pointer {
						quotedType = quotedType.Elem()
					}
					switch quotedType.Kind() {
					case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
						reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
						reflect.Float32, reflect.Float64, reflect.String:
						quoted = true
					}
				}
				old, found := selected[name]
				switch {
				case !found || len(path) < len(old.path) || (len(path) == len(old.path) && tagged && !old.tagged):
					selected[name] = field{path, tagged, parent.multiple, slices.Contains(optionsList, "omitzero"), slices.Contains(optionsList, "omitempty"), quoted}
				case len(path) == len(old.path) && tagged == old.tagged:
					old.ambiguous = true
					selected[name] = old
				}
			}
		}
		level = next
	}
	var fields []jsonField
	for name, field := range selected {
		if !field.ambiguous {
			encodedName, _ := json.Marshal(name)
			fields = append(fields, jsonField{field.path, encodedName, field.omitEmpty, field.omitZero, field.quoted})
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
