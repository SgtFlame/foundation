package services

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/nats-io/nats.go"
)

// CodeStream is 501: the operation hands back a channel, which travels as
// a stream (forge 21c).
const CodeStream = "service_stream"

// Spec is one service as the generated catalog describes it.
type Spec struct {
	Name      string       // the wire name
	Interface reflect.Type // <s>service.Service
	Ops       []OpSpec
	// Proxy makes the service's proxy, a <s>service.Service.
	Proxy func(c *Client) any
	// Serve serves svc, a <s>service.Service, on nc.
	Serve func(nc *nats.Conn, svc any, opts ...ServerOption) (*Server, error)
}

// OpSpec is one operation of a Spec.
type OpSpec struct {
	Name       string
	Idempotent bool
	Local      bool // only callable in the same process
	Stream     bool // hands back a channel
	Job        bool // may run for minutes
}

// NotServed is the response of op when it cannot be sent: 501 with code
// (CodeLocal or CodeStream). R is an <Op>Response, or a pointer to an
// <Op>Request whose channels deliver the failure: a channel of responses
// gets one, every other channel is closed.
func NotServed[R any](service, op, code string) R {
	var r R
	msg := fmt.Sprintf("%s.%s cannot be called from another process.", service, op)
	failure := Failed(http.StatusNotImplemented, code, msg)
	rv := reflect.ValueOf(&r).Elem()
	switch rv.Kind() {
	case reflect.Struct:
		SetCommon(&r, failure)
	case reflect.Pointer:
		req := reflect.New(rv.Type().Elem())
		fillChannels(req.Elem(), failure)
		rv.Set(req)
	}
	return r
}

// fillChannels gives every channel field of req a channel: one that holds
// responses gets failure, every other one is closed.
func fillChannels(req reflect.Value, failure any) {
	t := req.Type()
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i).Type
		if ft.Kind() != reflect.Chan || !t.Field(i).IsExported() {
			continue
		}
		both := reflect.ChanOf(reflect.BothDir, ft.Elem())
		ch := reflect.MakeChan(both, 1)
		elem := ft.Elem()
		resp := reflect.New(elem)
		target := resp.Elem()
		if elem.Kind() == reflect.Pointer {
			target = reflect.New(elem.Elem())
			resp.Elem().Set(target)
			target = target.Elem()
		}
		if c, ok := commonField(target); ok {
			c.Set(reflect.ValueOf(failure))
			ch.Send(resp.Elem())
		}
		ch.Close()
		req.Field(i).Set(ch.Convert(ft))
	}
}

// Refuse logs that op, which has no result and is only callable in the
// same process, was called through a proxy.
func Refuse(args any, service, op string) {
	log.Warn(ArgsOf(args).Context(), "%s.%s cannot be called from another process", service, op)
}
