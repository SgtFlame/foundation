package proxy

import (
	"encoding/json"
	"errors"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Resolved is config.Reader's: in this process when it runs the config
// service, else Resolve sent to a server of it (forge 21g-1). Written by
// hand beside the generated proxy; it wins over the embedded Reader's.
func (p *Proxy) Resolved(args features.ServiceArgs, section string) (json.RawMessage, error) {
	if p.Reader != nil {
		return p.Reader.Resolved(args, section)
	}
	resp := p.Resolve(configservice.ResolveArgs{ServiceArgs: args, Section: section})
	if resp.Common.Code != 200 {
		msg := resp.Common.Message
		if msg == "" {
			msg = "the config service could not resolve " + section
		}
		return nil, errors.New(msg)
	}
	return resp.Value, nil
}
