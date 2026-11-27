package infra

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Events of a configuration and of a version.
const (
	EventItemPut     = "itemPut"     // {kind, id, item}
	EventItemRemoved = "itemRemoved" // {kind, id}
	EventSettings    = "settingsSet" // {settings}
	EventImported    = "imported"    // {document}
	EventCommitted   = "committed"   // {number}
	EventApplied     = "applied"     // {number}
	EventVersion     = "created"     // a version's one event: {version}
)

// Config is a configuration as its projection keeps it: the draft, the
// last committed version's number and the last applied one's.
type Config struct {
	ID      string   `json:"id"`
	Draft   Document `json:"draft"`
	Head    int      `json:"head"`    // the last committed version, 0 for none
	Applied int      `json:"applied"` // the last applied version, 0 for none
	// Rev counts the draft's changes: what infra.config.commit's expect
	// names.
	Rev       uint64    `json:"rev"`
	Committed uint64    `json:"committedRev"` // the draft's rev at the last commit
	UpdatedAt time.Time `json:"updatedAt"`
}

// Dirty reports whether the draft changed since its last commit.
func (c Config) Dirty() bool { return c.Rev != c.Committed }

// Version is a committed configuration, which never changes.
type Version struct {
	Config    string    `json:"config"`
	Number    int       `json:"number"`
	Parent    int       `json:"parent"`
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	Document  Document  `json:"document"`
	CreatedAt time.Time `json:"createdAt"`
}

// VersionID is the target id of a configuration's version n.
func VersionID(config string, n int) string { return config + "-" + strconv.Itoa(n) }

type configEvent struct {
	Kind     string          `json:"kind,omitempty"`
	ID       string          `json:"id,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Settings *Settings       `json:"settings,omitempty"`
	Document *Document       `json:"document,omitempty"`
	Number   int             `json:"number,omitempty"`
	Version  *Version        `json:"version,omitempty"`
}

func foldConfig(f live.Fold) (json.RawMessage, error) {
	c := Config{ID: f.Event.Target.ID, Draft: Document{}.Normalized()}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &c); err != nil {
			return nil, err
		}
	}
	var e configEvent
	if err := json.Unmarshal(f.Event.Data, &e); err != nil {
		return nil, err
	}
	switch f.Event.Type {
	case EventItemPut:
		d, err := c.Draft.With(e.Kind, e.ID, e.Item)
		if err != nil {
			return nil, err
		}
		c.Draft = d
		c.Rev++
	case EventItemRemoved:
		d, err := c.Draft.With(e.Kind, e.ID, nil)
		if err != nil {
			return nil, err
		}
		c.Draft = d
		c.Rev++
	case EventSettings:
		if e.Settings != nil {
			c.Draft.Settings = *e.Settings
		}
		c.Rev++
	case EventImported:
		if e.Document != nil {
			c.Draft = e.Document.Normalized()
		}
		c.Rev++
	case EventCommitted:
		c.Head = e.Number
		c.Committed = c.Rev
	case EventApplied:
		c.Applied = e.Number
	default:
		return f.State, nil
	}
	c.UpdatedAt = f.Event.Time
	return json.Marshal(c)
}

func foldVersion(f live.Fold) (json.RawMessage, error) {
	if f.State != nil || f.Event.Type != EventVersion {
		return f.State, nil // a version never changes
	}
	var e configEvent
	if err := json.Unmarshal(f.Event.Data, &e); err != nil || e.Version == nil {
		return f.State, err
	}
	v := *e.Version
	v.CreatedAt = f.Event.Time
	return json.Marshal(v)
}

const configFields = `{id, draft: {accounts, sites, clusters, hostPools, profiles, stacks, placements, overrides, settings}, head, applied, rev, committedRev, updatedAt}`
const versionFields = `{config, number, parent, author, message, document, createdAt}`

func contributeConfig(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: ConfigProjector, Source: source}, Title: "Infrastructure configurations",
		Description: "Each infrastructure configuration's draft, keyed infraConfig.<id>: " + configFields,
		Area:        live.AreaDefinitions, Types: []string{ConfigType}, Fold: foldConfig,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: VersionProjector, Source: source}, Title: "Infrastructure configuration versions",
		Description: "Each committed configuration version, keyed infraVersion.<config>-<number>: " + versionFields,
		Area:        live.AreaDefinitions, Types: []string{VersionType}, Fold: foldVersion,
	})

	register := func(c live.Command, decide func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem)) {
		c.Source = source
		c.Area, c.TargetType, c.Projection = live.AreaDefinitions, ConfigType, ConfigProjector
		if c.Action == "" {
			c.Action = ActionAdmin
		}
		c.NewTarget = func() string { return DefaultConfig }
		c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if ps := rootOnly(d.Principal); ps != nil {
				return nil, ps
			}
			cfg := Config{ID: d.Target.ID, Draft: Document{}.Normalized()}
			if d.State != nil {
				if err := json.Unmarshal(d.State, &cfg); err != nil {
					return nil, problem("", "", "the configuration cannot be read: "+err.Error(), "")
				}
			}
			return decide(d, cfg)
		}
		live.Commands.Register(reg, c)
	}
	kinds := `{"type":"string","enum":["` + strings.Join(Kinds, `","`) + `"],"description":"Which list of the configuration: accounts, sites, clusters, hostPools, profiles, stacks, placements or overrides"}`

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.config.put"}, Title: "Add or change a configuration item",
		Description: "Puts one item into the infrastructure configuration's draft, replacing the item with the same id: a provider account, a site, a cluster, a host pool, an instance profile, a stack, a placement or a tenant override ({id, tenant, spec: {runners: {profile | stack, sites: [{site, count}], vm}}}: the tenant's own instances, in VMs of their own with vm). The whole draft is checked; a reference to an item that does not exist is refused. The target is the configuration, main when left out.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "kind":` + kinds + `,
  "item":{"type":"object","description":"The whole item, with its id (lower case letters, digits and dashes), e.g. a site {id, name, account, region}"}},"required":["kind","item"]}`),
		Examples: []json.RawMessage{
			json.RawMessage(`{"kind":"accounts","item":{"id":"test","name":"Test provider","provider":"infratest","connection":"c1"}}`),
			json.RawMessage(`{"kind":"hostPools","item":{"id":"general","site":"west","size":"m","count":3}}`),
		},
		Events: []string{EventItemPut},
	}, func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem) {
		var a struct {
			Kind string          `json:"kind"`
			Item json.RawMessage `json:"item"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		id := itemID(a.Item)
		if !ValidID(id) {
			return nil, problem("bad_config", "args.item.id", fmt.Sprintf("%q is not a valid id: lower case letters, digits and dashes, at most 63", id), "")
		}
		next, err := cfg.Draft.With(a.Kind, id, a.Item)
		if err != nil {
			return nil, problem("bad_config", "args.item", err.Error(), "")
		}
		if ps := next.Check(); len(ps) > 0 {
			return nil, ps
		}
		old := cfg.Draft.Item(a.Kind, id)
		item := next.Item(a.Kind, id)
		if old != nil && string(old) == string(item) {
			d.Undo("")
			return nil, nil
		}
		if old == nil {
			d.Undo("", live.OpOf("infra.config.remove", d.Target.ID, map[string]any{"kind": a.Kind, "id": id}))
		} else {
			d.Undo("", live.OpOf("infra.config.put", d.Target.ID, map[string]any{"kind": a.Kind, "item": old}))
		}
		return []live.NewEvent{{Type: EventItemPut, Data: configEvent{Kind: a.Kind, ID: id, Item: item}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.config.remove"}, Title: "Remove a configuration item",
		Description: "Removes one item from the configuration's draft. Refused while another item refers to it (remove that first).",
		Args:        json.RawMessage(`{"type":"object","properties":{"kind":` + kinds + `,"id":{"type":"string","description":"The item's id"}},"required":["kind","id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"kind":"hostPools","id":"general"}`)},
		Events:      []string{EventItemRemoved},
	}, func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem) {
		var a struct{ Kind, ID string }
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		old := cfg.Draft.Item(a.Kind, a.ID)
		if old == nil {
			if !isKind(a.Kind) {
				return nil, problem("bad_config", "args.kind", fmt.Sprintf("%q is not a kind of item", a.Kind), "")
			}
			d.Undo("")
			return nil, nil
		}
		next, err := cfg.Draft.With(a.Kind, a.ID, nil)
		if err != nil {
			return nil, problem("bad_config", "args", err.Error(), "")
		}
		if ps := next.Check(); len(ps) > 0 {
			return nil, ps
		}
		d.Undo("", live.OpOf("infra.config.put", d.Target.ID, map[string]any{"kind": a.Kind, "item": old}))
		return []live.NewEvent{{Type: EventItemRemoved, Data: configEvent{Kind: a.Kind, ID: a.ID}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.config.settings"}, Title: "Change the configuration's settings",
		Description: "Sets the configuration's own settings, all of them at once: onDrift, report (the default) or correct, which says what the drift job does when actual state leaves the applied version; crossSite, the services a serve unit may call on another site across a gateway.",
		Args:        json.RawMessage(`{"type":"object","properties":{"onDrift":{"type":"string","enum":["report","correct"],"description":"report: only say so; correct: apply the version again"},"crossSite":{"type":"array","items":{"type":"string"},"description":"Services (wire names) serve units may call on another site, across a gateway; none by default"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"onDrift":"correct"}`)},
		Events:      []string{EventSettings},
	}, func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem) {
		var s Settings
		if err := json.Unmarshal(d.Args, &s); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		next := cfg.Draft.Clone()
		next.Settings = s
		if ps := next.Check(); len(ps) > 0 {
			return nil, ps
		}
		if s.Equal(cfg.Draft.Settings) {
			d.Undo("")
			return nil, nil
		}
		d.Undo("", live.OpOf("infra.config.settings", d.Target.ID, cfg.Draft.Settings))
		return []live.NewEvent{{Type: EventSettings, Data: configEvent{Settings: &s}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.config.import"}, Title: "Replace the configuration's draft",
		Description: "Replaces the whole draft with a document (or an export's files), such as an exported version read back (infra.config.export) or what the seed made. Refused while the draft has changes not committed, unless force.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "document":{"type":"object","description":"The whole configuration: {accounts, sites, clusters, hostPools, profiles, stacks, placements, overrides, settings}"},
  "files":{"type":"object","additionalProperties":{"type":"string"},"description":"In place of document: an export's YAML files by path (infra.config.export's files)"},
  "force":{"type":"boolean","description":"Replace the draft even though it has changes not committed"}}}`),
		Examples: []json.RawMessage{json.RawMessage(`{"document":{"accounts":[{"id":"test","name":"Test","provider":"infratest"}],"sites":[{"id":"west","name":"West","account":"test","region":"r1"}]}}`)},
		Events:   []string{EventImported},
	}, func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem) {
		var a struct {
			Document json.RawMessage   `json:"document"`
			Files    map[string]string `json:"files"`
			Force    bool              `json:"force"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		var doc Document
		switch {
		case len(a.Files) > 0 && len(a.Document) > 0:
			return nil, problem("", "args.files", "send document or files, not both", "")
		case len(a.Files) > 0:
			var ps []live.Problem
			if doc, ps = Import(a.Files); len(ps) > 0 {
				return nil, ps
			}
		case len(a.Document) > 0:
			var err error
			if doc, err = DecodeDocument(a.Document); err != nil {
				return nil, problem("bad_config", "args.document", err.Error(), "")
			}
			if ps := SecretProblems(doc); len(ps) > 0 {
				return nil, ps
			}
		default:
			return nil, problem("", "args.document", "send the document, or an export's files", "")
		}
		if ps := doc.Check(); len(ps) > 0 {
			return nil, ps
		}
		if cfg.Dirty() && cfg.Rev > 0 && !a.Force {
			return nil, problem("draft_dirty", "args.force", "the draft has changes that are not committed", "commit them, or send force: true to replace them")
		}
		d.Undo("", live.OpOf("infra.config.import", d.Target.ID, map[string]any{"document": cfg.Draft, "force": true}))
		return []live.NewEvent{{Type: EventImported, Data: configEvent{Document: &doc}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.config.commit"}, Title: "Commit the configuration",
		Description: "Commits the draft as a new immutable configuration version with a message, which can then be planned and applied. expect is the draft's rev the committer saw (infra.config's rev): if the draft changed since, the commit is refused with head_moved.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "message":{"type":"string","description":"What the version changes, e.g. Add an Azure site of three hosts"},
  "expect":{"type":"integer","minimum":0,"description":"The draft's rev the committer saw"},
  "applied":{"type":"boolean","description":"Also mark the new version applied, for a draft that describes what exists already (the seed's first version, forge 22o)"}},"required":["message","expect"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"message":"Add the west site","expect":4}`)},
		Events:   []string{EventCommitted, EventVersion, EventApplied},
		NoBranch: true,
	}, func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem) {
		var a struct {
			Message string `json:"message"`
			Expect  uint64 `json:"expect"`
			Applied bool   `json:"applied"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		if strings.TrimSpace(a.Message) == "" {
			return nil, problem("", "args.message", "a commit needs a message", "")
		}
		if a.Expect != cfg.Rev {
			return nil, problem("head_moved", "args.expect", fmt.Sprintf("the draft moved: it is at rev %d, not %d", cfg.Rev, a.Expect), "read infra.config again and look at what changed before committing")
		}
		if cfg.Head > 0 && !cfg.Dirty() {
			return nil, problem("nothing_to_commit", "", fmt.Sprintf("the draft is version %d as committed", cfg.Head), "")
		}
		if ps := cfg.Draft.Check(); len(ps) > 0 {
			return nil, ps
		}
		n := cfg.Head + 1
		vt := live.Target{Area: live.AreaDefinitions, Type: VersionType, ID: VersionID(d.Target.ID, n)}
		if prev, err := d.Read(VersionProjector, vt); err == nil && prev != nil {
			return nil, problem("head_moved", "", fmt.Sprintf("version %d exists", n), "read infra.config again")
		}
		d.NoUndo("", "a committed version is permanent")
		v := Version{Config: d.Target.ID, Number: n, Parent: cfg.Head, Author: authorOf(d.Principal), Message: strings.TrimSpace(a.Message), Document: cfg.Draft.Normalized()}
		evs := []live.NewEvent{
			{Type: EventCommitted, Data: configEvent{Number: n}},
			{Type: EventVersion, Data: configEvent{Version: &v}, Target: &vt},
		}
		if a.Applied {
			evs = append(evs, live.NewEvent{Type: EventApplied, Data: configEvent{Number: n}})
		}
		return evs, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "infra.config.markApplied"}, Title: "Mark a version applied",
		Description: "Records that an apply reached a version (sent by the apply when its last step is done).",
		Args:        json.RawMessage(`{"type":"object","properties":{"number":{"type":"integer","minimum":1,"description":"The version applied"}},"required":["number"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"number":2}`)},
		Events:      []string{EventApplied},
		Internal:    true,
		NoBranch:    true,
	}, func(d live.Decision, cfg Config) ([]live.NewEvent, []live.Problem) {
		var a struct {
			Number int `json:"number"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		if a.Number < 1 || a.Number > cfg.Head {
			return nil, problem("version_not_found", "args.number", fmt.Sprintf("there is no version %d", a.Number), "")
		}
		d.NoUndo("", "what was applied is a fact")
		if cfg.Applied == a.Number {
			return nil, nil
		}
		return []live.NewEvent{{Type: EventApplied, Data: configEvent{Number: a.Number}}}, nil
	})

	configKey := func(f live.Feed) string {
		var p struct {
			Config string `json:"config"`
		}
		_ = json.Unmarshal(f.Params(), &p)
		if p.Config == "" || !live.ValidToken(p.Config) {
			p.Config = DefaultConfig
		}
		return p.Config
	}
	configParam := `"config":{"type":"string","description":"The configuration, main when left out"}`
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.config", Source: source}, Title: "The infrastructure configuration",
		Description: "The configuration's draft with its head (last committed version), applied version and rev, or null before its first change: " + configFields,
		Params:      json.RawMessage(`{"type":"object","properties":{` + configParam + `}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      ActionRead,
		Run: live.ValueOf(ConfigProjector, func(f live.Feed) (string, error) {
			return ConfigType + "." + configKey(f), nil
		}, json.RawMessage(`null`)),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.versions", Source: source}, Title: "Configuration versions",
		Description: "Every committed version of the configuration, keyed infraVersion.<config>-<number>: " + versionFields,
		Params:      json.RawMessage(`{"type":"object","properties":{` + configParam + `}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      ActionRead,
		// A KV pattern matches whole tokens only, so every version is
		// watched and those of other configurations dropped.
		Run: func(f live.Feed) error {
			prefix := VersionType + "." + configKey(f) + "-"
			return live.ListOf(VersionProjector, func(live.Feed) (string, error) { return VersionType + ".*", nil })(prefixFeed{f, prefix})
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.version", Source: source}, Title: "A configuration version",
		Description: "One committed version, or null: " + versionFields,
		Params:      json.RawMessage(`{"type":"object","properties":{` + configParam + `,"number":{"type":"integer","minimum":1,"description":"The version's number"}},"required":["number"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"number":1}`)},
		Action:      ActionRead,
		Run: live.ValueOf(VersionProjector, func(f live.Feed) (string, error) {
			var p struct {
				Number int `json:"number"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || p.Number < 1 {
				return "", fmt.Errorf("infra.version needs a version's number")
			}
			return VersionType + "." + VersionID(configKey(f), p.Number), nil
		}, json.RawMessage(`null`)),
	})
}

// authorOf names who committed: a user's id as text, else the principal's
// kind.
func authorOf(p features.Principal) string {
	if p.UserID != 0 {
		return strconv.FormatUint(p.UserID, 10)
	}
	return string(p.Kind)
}

// DecodeDocument reads a configuration document, refusing fields it does
// not know.
func DecodeDocument(raw json.RawMessage) (Document, error) {
	var d Document
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, err
	}
	return d.Normalized(), nil
}

// prefixFeed passes on only the keys with its prefix.
type prefixFeed struct {
	live.Feed
	prefix string
}

func (p prefixFeed) Snapshot(data any, pos live.Position) {
	if m, ok := data.(map[string]json.RawMessage); ok {
		kept := map[string]json.RawMessage{}
		for k, v := range m {
			if strings.HasPrefix(k, p.prefix) {
				kept[k] = v
			}
		}
		data = kept
	}
	p.Feed.Snapshot(data, pos)
}

func (p prefixFeed) Change(d live.Delta, pos live.Position) {
	if d.Key != "" && !strings.HasPrefix(d.Key, p.prefix) {
		d = live.Delta{Op: "reached"}
	}
	p.Feed.Change(d, pos)
}
