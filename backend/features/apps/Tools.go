package apps

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Tool is a tool the app gives the model (forge 07j): its function graph
// runs a call, the graph's On call outputs being the tool's arguments and
// its Return inputs the result. Installed, it is the tool <appId>.<name>,
// run as the calling principal: the user in the chat (asking first for a
// write), an agent in the agent's runs.
type Tool struct {
	Name        string `json:"name"`
	Graph       string `json:"graph"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	// Effect is none, read (the default), write or external: write and
	// external ask a user before they run.
	Effect string `json:"effect,omitempty"`
}

// ToolID is a tool's installed name: <appId>.<name>.
func (t Tool) ToolID(app string) string { return app + "." + t.Name }

// toolName is one word: the model sees <appId>_<name>.
var toolName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// ToolEffects are the effects a manifest tool may have.
var ToolEffects = []string{"none", "read", "write", "external"}

// ToolGraphs are the graphs a manifest's tools run.
func (m Manifest) ToolGraphs() []string {
	var out []string
	for _, t := range m.Tools {
		if t.Graph != "" && !slices.Contains(out, t.Graph) {
			out = append(out, t.Graph)
		}
	}
	return out
}

func (c *check) tools() {
	seen := map[string]bool{}
	for _, t := range c.m.Tools {
		path := "tools[" + t.Name + "]"
		switch {
		case !toolName.MatchString(t.Name):
			c.add(path+".name", fmt.Sprintf("%q is not a tool name", t.Name), "use one word of letters and digits, such as readThread")
		case seen[t.Name]:
			c.add(path+".name", fmt.Sprintf("the tool %s is declared twice", t.Name), "")
		case len(t.ToolID(c.m.ID)) > 64:
			c.add(path+".name", fmt.Sprintf("the tool's name %s is longer than 64 characters", t.ToolID(c.m.ID)), "shorten it")
		}
		seen[t.Name] = true
		if t.Description == "" {
			c.add(path+".description", "a tool needs a description: what it does and when to call it, for a model", "")
		}
		if t.Effect != "" && !slices.Contains(ToolEffects, t.Effect) {
			c.add(path+".effect", fmt.Sprintf("there is no effect %q", t.Effect), "use none, read, write or external")
		}
		var g cognition.Graph
		ok := false
		if c.env.Graph != nil && live.ValidToken(t.Graph) {
			g, ok = c.env.Graph(t.Graph)
		}
		switch {
		case !ok:
			c.add(path+".graph", fmt.Sprintf("there is no graph %q", t.Graph), "make a function graph with bp.create first: its On call outputs are the tool's arguments, its Return the result")
		case g.Kind != cognition.KindFunction:
			c.add(path+".graph", fmt.Sprintf("the graph %s is a %s graph, not a function graph", t.Graph, g.Kind), "name a function graph")
		case g.Owner != Owner(c.m.ID):
			c.add(path+".graph", fmt.Sprintf("the graph %s is not this app's own", t.Graph), "make it with owner app:"+c.m.ID)
		}
	}
}

// Tool is one of the version's tools with its pinned graph.
func (v Version) Tool(name string) (Tool, cognition.Graph, bool) {
	i := slices.IndexFunc(v.Manifest.Tools, func(t Tool) bool { return t.Name == name })
	if i < 0 {
		return Tool{}, cognition.Graph{}, false
	}
	t := v.Manifest.Tools[i]
	g, ok := v.Graphs[t.Graph]
	return t, g, ok
}
