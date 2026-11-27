package services

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
)

var (
	jsonMarshaler = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// CannotTravel lists why each operation of spec that is not local cannot
// travel: a field of its args or response, at any depth, holding a
// channel, a function or an interface with methods (a reader, a database
// handle). A streamed operation's request may hold channels at its top
// level (forge 21c); their values and its other fields must travel.
// Empty when every one can.
func CannotTravel(spec Spec) []string {
	var out []string
	for _, op := range spec.Ops {
		if op.Local {
			continue
		}
		m, ok := spec.Interface.MethodByName(op.Name)
		if !ok {
			out = append(out, fmt.Sprintf("%s.%s: not on the interface", spec.Name, op.Name))
			continue
		}
		types := []reflect.Type{m.Type.In(0), m.Type.Out(0)}
		if op.Stream {
			types = streamParts(m.Type.In(0), m.Type.Out(0))
		}
		for _, t := range types {
			if why := travels(t, t.Name(), map[reflect.Type]bool{}); why != "" {
				out = append(out, fmt.Sprintf("%s.%s: %s cannot travel; mark the operation //svc:local or reshape it", spec.Name, op.Name, why))
			}
		}
	}
	return out
}

func travels(t reflect.Type, path string, seen map[reflect.Type]bool) string {
	if t == argsType {
		return ""
	}
	if seen[t] {
		return ""
	}
	seen[t] = true
	if t.Implements(jsonMarshaler) || reflect.PointerTo(t).Implements(jsonMarshaler) ||
		t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) {
		return ""
	}
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Complex64, reflect.Complex128:
		return fmt.Sprintf("%s (%s)", path, t)
	case reflect.Interface:
		if t.NumMethod() > 0 {
			return fmt.Sprintf("%s (%s)", path, t)
		}
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return travels(t.Elem(), path, seen)
	case reflect.Map:
		if why := travels(t.Key(), path+"[key]", seen); why != "" {
			return why
		}
		return travels(t.Elem(), path+"[]", seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" {
				continue
			}
			if why := travels(f.Type, path+"."+f.Name, seen); why != "" {
				return why
			}
		}
	}
	return ""
}

// streamParts are what a streamed operation sends: its args, and its
// request's fields with each channel's values in place of the channel.
func streamParts(args, req reflect.Type) []reflect.Type {
	out := []reflect.Type{args}
	if req.Kind() == reflect.Pointer {
		req = req.Elem()
	}
	if req.Kind() != reflect.Struct {
		return append(out, req)
	}
	for i := 0; i < req.NumField(); i++ {
		f := req.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type.Kind() == reflect.Chan {
			out = append(out, f.Type.Elem())
		} else {
			out = append(out, f.Type)
		}
	}
	return out
}
