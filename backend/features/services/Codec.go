package services

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/SgtFlame/foundation/backend/features"
)

var (
	argsType   = reflect.TypeOf(features.ServiceArgs{})
	commonType = reflect.TypeOf(features.ServiceResponse{})
)

// Encode is an operation's args or response as they travel: JSON, with
// every embedded features.ServiceArgs left out (its context, principal and
// meta travel as headers) and every Common's Trace left out.
func Encode(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		cp := reflect.New(rv.Type()).Elem()
		cp.Set(rv)
		if f, ok := serviceArgsField(cp); ok {
			f.Set(reflect.Zero(argsType))
		}
		if f, ok := commonField(cp); ok {
			f.FieldByName("Trace").SetString("")
		}
		v = cp.Interface()
	}
	return json.Marshal(v)
}

// Decode reads a body Encode wrote into v (a pointer).
func Decode(body []byte, v any) error {
	return json.Unmarshal(body, v)
}

// ArgsOf is the ServiceArgs embedded in an operation's args (the zero
// value when it embeds none).
func ArgsOf(args any) features.ServiceArgs {
	rv := reflect.ValueOf(args)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if f, ok := serviceArgsField(rv); ok {
		return f.Interface().(features.ServiceArgs)
	}
	return features.ServiceArgs{}
}

// setArgs sets the ServiceArgs embedded in *args.
func setArgs(args any, a features.ServiceArgs) {
	if f, ok := serviceArgsField(reflect.ValueOf(args).Elem()); ok {
		f.Set(reflect.ValueOf(a))
	}
}

// SetCommon sets the Common field of *resp, the operation's
// features.ServiceResponse.
func SetCommon(resp any, c features.ServiceResponse) {
	if f, ok := commonField(reflect.ValueOf(resp).Elem()); ok {
		f.Set(reflect.ValueOf(c))
	}
}

// CommonOf is the Common field of resp (a value or a pointer).
func CommonOf(resp any) features.ServiceResponse {
	rv := reflect.ValueOf(resp)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return features.ServiceResponse{}
		}
		rv = rv.Elem()
	}
	if f, ok := commonField(rv); ok {
		return f.Interface().(features.ServiceResponse)
	}
	return features.ServiceResponse{}
}

func serviceArgsField(v reflect.Value) (reflect.Value, bool) {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Type == argsType {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func commonField(v reflect.Value) (reflect.Value, bool) {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	f := v.FieldByName("Common")
	if !f.IsValid() || f.Type() != commonType {
		return reflect.Value{}, false
	}
	return f, true
}

// mustStruct panics when T is not a struct: the generated code only
// instantiates the helpers with an operation's args and response types.
func mustStruct[T any]() {
	var t T
	if reflect.TypeOf(t).Kind() != reflect.Struct {
		panic(fmt.Sprintf("services: %T is not a struct", t))
	}
}
