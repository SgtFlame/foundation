// Package providers holds what every infra.compute@1 provider type shares
// (forge 22g, 22l to 22n; iteration question 1: Go types, as Mail's are):
// a Backend each provider implements over its API, the session that turns
// contract calls into Backend calls (keyed creates, idempotent deletes,
// argument checks), and an HTTP client mapping API statuses to connector
// error codes. Each provider is a package below, tested with
// contracttest against recorded responses (providers/cassette).
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

// CreateArgs are host.create's arguments.
type CreateArgs struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Size     string `json:"size"`
	Image    string `json:"image,omitempty"`
	Region   string `json:"region"`
	UserData string `json:"userData,omitempty"`
	Nested   bool   `json:"nested,omitempty"`
}

// Named is an id and a name (an image or a region).
type Named struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Backend is one provider account's API, as infra.compute@1 needs it.
// Hosts are found by our resource id (a tag, label or name); Get answers
// a *connectors.Error notFound for a host that is not there.
type Backend interface {
	Check(ctx context.Context) error
	Sizes(ctx context.Context, region string) ([]infra.ProviderSize, error)
	Images(ctx context.Context) ([]Named, error)
	Regions(ctx context.Context) ([]Named, error)
	Create(ctx context.Context, in CreateArgs) (infra.ProviderHost, error)
	Get(ctx context.Context, id string) (infra.ProviderHost, error)
	List(ctx context.Context, region string) ([]infra.ProviderHost, error)
	Start(ctx context.Context, id string) (infra.ProviderHost, error)
	Stop(ctx context.Context, id string) (infra.ProviderHost, error)
	// Delete deletes the host with what was made for it; false when there
	// was none.
	Delete(ctx context.Context, id string) (bool, error)
}

// Session serves infra.compute@1 over a Backend.
type Session struct{ B Backend }

// Close does nothing: backends hold no connection.
func (Session) Close() error { return nil }

// NotFound tells a notFound error.
func NotFound(err error) bool {
	var ce *connectors.Error
	return errors.As(err, &ce) && ce.Code == connectors.CodeNotFound
}

// Call runs one operation.
func (s Session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		CreateArgs
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "the arguments are not valid: %v", err)
		}
	}
	needID := func() error {
		if in.ID == "" {
			return connectors.Errorf(connectors.CodeInvalid, "id is required")
		}
		return nil
	}
	switch op {
	case "check":
		if err := s.B.Check(ctx); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"ok":true}`), nil
	case "size.list":
		sizes, err := s.B.Sizes(ctx, in.Region)
		if err != nil {
			return nil, err
		}
		if sizes == nil {
			sizes = []infra.ProviderSize{}
		}
		return json.Marshal(map[string]any{"sizes": sizes})
	case "image.list":
		images, err := s.B.Images(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"images": nonNil(images)})
	case "region.list":
		regions, err := s.B.Regions(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"regions": nonNil(regions)})
	case "host.create":
		if err := needID(); err != nil {
			return nil, err
		}
		if in.Size == "" || in.Region == "" {
			return nil, connectors.Errorf(connectors.CodeInvalid, "size and region are required")
		}
		if h, err := s.B.Get(ctx, in.ID); err == nil && h.State != infra.HostGone {
			return host(h)
		} else if err != nil && !NotFound(err) {
			return nil, err
		}
		if in.Nested {
			sizes, err := s.B.Sizes(ctx, in.Region)
			if err != nil {
				return nil, err
			}
			ok := false
			for _, z := range sizes {
				ok = ok || (z.ID == in.Size && z.VMs)
			}
			if !ok {
				return nil, connectors.Errorf(connectors.CodeInvalid, "size %s cannot run virtual machines", in.Size)
			}
		}
		if in.Name == "" {
			in.Name = in.ID
		}
		h, err := s.B.Create(ctx, in.CreateArgs)
		if err != nil {
			return nil, err
		}
		return host(h)
	case "host.get", "host.start", "host.stop":
		if err := needID(); err != nil {
			return nil, err
		}
		var h infra.ProviderHost
		var err error
		switch op {
		case "host.get":
			h, err = s.B.Get(ctx, in.ID)
		case "host.start":
			h, err = s.B.Start(ctx, in.ID)
		default:
			h, err = s.B.Stop(ctx, in.ID)
		}
		if err != nil {
			return nil, err
		}
		return host(h)
	case "host.list":
		hosts, err := s.B.List(ctx, in.Region)
		if err != nil {
			return nil, err
		}
		out := []infra.ProviderHost{}
		for _, h := range hosts {
			if h.State != infra.HostGone {
				out = append(out, normal(h))
			}
		}
		return json.Marshal(map[string]any{"hosts": out})
	case "host.delete":
		if err := needID(); err != nil {
			return nil, err
		}
		deleted, err := s.B.Delete(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"deleted": deleted})
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
}

func nonNil(n []Named) []Named {
	if n == nil {
		return []Named{}
	}
	return n
}

func normal(h infra.ProviderHost) infra.ProviderHost {
	if h.Addresses == nil {
		h.Addresses = []string{}
	}
	return h
}

func host(h infra.ProviderHost) (json.RawMessage, error) { return json.Marshal(normal(h)) }

// Hostname is a resource id as a hostname (letters, digits and -, at most
// 63, not starting with a digit), as providers name machines.
func Hostname(id string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	n := strings.Trim(sb.String(), "-")
	if len(n) > 63 {
		n = strings.TrimRight(n[:63], "-")
	}
	if n == "" || (n[0] >= '0' && n[0] <= '9') {
		n = "h-" + n
	}
	return n
}
