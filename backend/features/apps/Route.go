package apps

import (
	"strings"
)

// MatchRoute matches a path (/task/123) against a page's route
// (/task/:id), giving the route's params.
func MatchRoute(route, path string) (map[string]string, bool) {
	if !strings.HasPrefix(route, "/") || !strings.HasPrefix(path, "/") {
		return nil, false
	}
	rs, ps := strings.Split(strings.TrimSuffix(route, "/"), "/"), strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(rs) != len(ps) {
		return nil, false
	}
	params := map[string]string{}
	for i, seg := range rs {
		if name, ok := strings.CutPrefix(seg, ":"); ok {
			if ps[i] == "" {
				return nil, false
			}
			params[name] = ps[i]
		} else if seg != ps[i] {
			return nil, false
		}
	}
	return params, true
}

// FindPage is the page a path opens, with the route's params.
func (m Manifest) FindPage(path string) (Page, map[string]string, bool) {
	for _, p := range m.Pages {
		if params, ok := MatchRoute(p.Route, path); ok {
			return p, params, true
		}
	}
	return Page{}, nil, false
}

// HomeRoute is the route the app opens on (forge 05d): its home, else its
// first page without params; empty when every page needs params, and the
// app then has no main tab.
func (m Manifest) HomeRoute() string {
	if m.Home != "" {
		return m.Home
	}
	for _, p := range m.Pages {
		if params, err := RouteParams(p.Route); err == nil && len(params) == 0 {
			return p.Route
		}
	}
	return ""
}

// KindFor is the editor kind showing a page's route, if one does.
func (m Manifest) KindFor(route string) (EditorKind, bool) {
	for _, k := range m.EditorKinds {
		if k.Page == route {
			return k, true
		}
	}
	return EditorKind{}, false
}

// FillRoute fills a route's params: /task/:id with {id: 123} is /task/123.
func FillRoute(route string, params map[string]string) string {
	segs := strings.Split(route, "/")
	for i, seg := range segs {
		if name, ok := strings.CutPrefix(seg, ":"); ok {
			segs[i] = params[name]
		}
	}
	return strings.Join(segs, "/")
}
