package configdto

import "github.com/SgtFlame/foundation/backend/features/config/services/configservice"

// View is a resolved section; secrets appear as {set, hint}.
type View struct {
	Section  string            `json:"section,omitempty"`
	Scope    string            `json:"scope,omitempty"`
	Value    map[string]any    `json:"value,omitempty"`
	Sources  map[string]string `json:"sources,omitempty"`
	Revision int64             `json:"revision"`
}

func ViewFrom(v configservice.View) View {
	return View{Section: v.Section, Scope: string(v.Scope), Value: v.Value, Sources: v.Sources, Revision: v.Revision}
}
