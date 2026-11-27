package services

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Groups are named service sets (forge 21g-1): serve --services takes
// services and groups. A service may be in several groups.
type Groups map[string][]string

// FoundationGroups are Foundation's groups. web holds the HTTP entry
// points, the browser's live connection and UI sessions, identity's
// collab rooms (21g-1's question 1: session refresh stays with identity in data until Authenticate and Sessions call it, so web's middleware reads the application database meanwhile); live is in
// web too until the browser's socket calls svc.live when the core is
// elsewhere (21d-4). data holds every data service; runner the AI calls
// and the index. connectors:<type> names one adapter type and is kept
// as given.
func FoundationGroups() Groups {
	return Groups{
		"web":    {"extensions", "ui", "collab", "kb", "volume", "live"},
		"data":   {"identity", "tenant", "user", "authz", "secrets", "config", "usage", "conversation", "chronicle", "retrieval", "cognition", "entity", "prefab", "app", "record", "files", "live", "connectors", "logs"},
		"runner": {"chat", "embed", "index", "parse"},
	}
}

// With is g with more services added to its groups (Forge adds its own).
func (g Groups) With(more Groups) Groups {
	out := Groups{}
	for k, v := range g {
		out[k] = slices.Clone(v)
	}
	for k, v := range more {
		out[k] = append(out[k], v...)
	}
	return out
}

// ParseSet reads a --services value: comma-separated services (from
// known, the wire names) and groups. "all" or "" is nil: every service.
func ParseSet(spec string, groups Groups, known []string) (map[string]bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "all" {
		return nil, nil
	}
	set := map[string]bool{}
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		switch {
		case item == "":
		case item == "all":
			return nil, nil
		case strings.HasPrefix(item, "connectors:"):
			set[item] = true
		case groups[item] != nil:
			for _, s := range groups[item] {
				set[s] = true
			}
		case slices.Contains(known, item):
			set[item] = true
		default:
			names := append(slices.Sorted(maps.Keys(groups)), known...)
			return nil, fmt.Errorf("--services: %q is neither a service nor a group (%s)", item, strings.Join(names, ", "))
		}
	}
	return set, nil
}

// Elsewhere are the services of known a set does not run: what other
// processes must run (serve --services … --check).
func Elsewhere(set map[string]bool, known []string) []string {
	if set == nil {
		return nil
	}
	var out []string
	for _, k := range known {
		if !set[k] {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
