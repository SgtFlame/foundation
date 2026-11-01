package impl

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

type service struct {
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	deps data.Deps
}

// command sends one record command; the bus checks access.
func (s *service) command(args features.ServiceArgs, id, typ, target string, a any) recordservice.CommandResponse {
	raw, _ := json.Marshal(a)
	if id == "" {
		id = fmt.Sprintf("rs-%d", features.NewTSID())
	}
	res := s.bus.Execute(bus.Request{ServiceArgs: args, ID: id, Type: typ, Target: target, Args: raw})
	return recordservice.CommandResponse{Common: res.Common, ID: res.Target, Seq: res.Seq, Problems: res.Problems}
}

// once reads a query once; the subscriptions check access.
func (s *service) once(args features.ServiceArgs, query string, params any) (json.RawMessage, *features.ServiceResponse) {
	raw, _ := json.Marshal(params)
	data, _, fail := s.subs.Once(args, query, raw)
	if fail != nil {
		return nil, fail
	}
	out, _ := json.Marshal(data)
	return out, nil
}
