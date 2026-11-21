package uidto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

type App struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Icon        string `json:"icon,omitempty"`
	// Reach is what a user needs to use it, any one of them; resources are
	// below the tenant. None: everyone who may read the UI.
	Reach []ui.Reach `json:"reach"`
}

// EditorKind is one kind of top-level app tab; ID is "app:<appId>:<kind>".
type EditorKind struct {
	ID    string `json:"id"`
	App   string `json:"app"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Page  string `json:"page"`
}

type EditorKindsResponse struct {
	rest.Status
	Apps  []App        `json:"apps"`
	Kinds []EditorKind `json:"kinds"`
}

func EditorKindsResponseFrom(resp uiservice.EditorKindsResponse) EditorKindsResponse {
	out := EditorKindsResponse{Status: rest.StatusFrom(resp.Common), Apps: []App{}, Kinds: []EditorKind{}}
	for _, a := range resp.Apps {
		reach := a.Reach
		if reach == nil {
			reach = []ui.Reach{}
		}
		out.Apps = append(out.Apps, App{ID: a.ID, Title: a.Title, Description: a.Description, Icon: a.Icon, Reach: reach})
	}
	for _, k := range resp.Kinds {
		out.Kinds = append(out.Kinds, EditorKind{ID: k.ID, App: k.App, Kind: k.Kind, Title: k.Title, Icon: k.Icon, Page: k.Page})
	}
	return out
}
