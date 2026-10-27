package cognitiondto

import "github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"

// GraphInfo is a graph without its nodes.
type GraphInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Version     int    `json:"version"`
	BuiltIn     bool   `json:"builtIn"`
}

// GraphInfoFrom converts the service's GraphInfo.
func GraphInfoFrom(g cognitionservice.GraphInfo) GraphInfo {
	return GraphInfo{ID: g.ID, Name: g.Name, Description: g.Description, Kind: g.Kind, Version: g.Version, BuiltIn: g.BuiltIn}
}
