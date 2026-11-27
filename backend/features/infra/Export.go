package infra

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// The export's layout (forge 22b): config.yaml holds the accounts,
// profiles, tenant overrides and settings; sites/<id>.yaml a site with its
// cluster and host pools; stacks/<id>.yaml a stack; placements.yaml the
// placements. Files are YAML read back by Import into the same document.
const (
	fileConfig     = "config.yaml"
	fileSites      = "sites/"
	fileStacks     = "stacks/"
	filePlacements = "placements.yaml"
)

type exportConfig struct {
	Accounts  []Account  `json:"accounts,omitempty"`
	Profiles  []Profile  `json:"profiles,omitempty"`
	Overrides []Override `json:"overrides,omitempty"`
	Settings  *Settings  `json:"settings,omitempty"`
}

type exportSite struct {
	Site      Site       `json:"site"`
	Cluster   *Cluster   `json:"cluster,omitempty"`
	HostPools []HostPool `json:"hostPools,omitempty"`
}

type exportPlacements struct {
	Placements []Placement `json:"placements"`
}

// Export is a document as files, path → YAML. It is refused with the
// problems SecretProblems finds: an export may be committed to a
// repository, so nothing that looks like a secret ever leaves.
func Export(doc Document) (map[string]string, []live.Problem) {
	doc = doc.Normalized()
	if ps := SecretProblems(doc); len(ps) > 0 {
		return nil, ps
	}
	files := map[string]string{}
	put := func(name string, v any) []live.Problem {
		s, err := toYAML(v)
		if err != nil {
			return problem("", name, err.Error(), "")
		}
		files[name] = s
		return nil
	}
	cfg := exportConfig{Accounts: doc.Accounts, Profiles: doc.Profiles, Overrides: doc.Overrides}
	if doc.Settings != (Settings{}) {
		cfg.Settings = &doc.Settings
	}
	if ps := put(fileConfig, cfg); ps != nil {
		return nil, ps
	}
	for _, s := range doc.Sites {
		es := exportSite{Site: s}
		for i := range doc.Clusters {
			if doc.Clusters[i].Site == s.ID {
				c := doc.Clusters[i]
				es.Cluster = &c
			}
		}
		for _, h := range doc.HostPools {
			if h.Site == s.ID {
				es.HostPools = append(es.HostPools, h)
			}
		}
		if ps := put(fileSites+s.ID+".yaml", es); ps != nil {
			return nil, ps
		}
	}
	for _, s := range doc.Stacks {
		if ps := put(fileStacks+s.ID+".yaml", s); ps != nil {
			return nil, ps
		}
	}
	if len(doc.Placements) > 0 {
		if ps := put(filePlacements, exportPlacements{doc.Placements}); ps != nil {
			return nil, ps
		}
	}
	return files, nil
}

// Import reads an export back: the document its files hold, checked for
// secrets but not otherwise (the import command checks it whole).
func Import(files map[string]string) (Document, []live.Problem) {
	var doc Document
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := "files." + name
		clean := path.Clean(name)
		switch {
		case clean == fileConfig:
			var c exportConfig
			if err := fromYAML(files[name], &c); err != nil {
				return Document{}, problem("bad_config", p, err.Error(), "")
			}
			doc.Accounts = append(doc.Accounts, c.Accounts...)
			doc.Profiles = append(doc.Profiles, c.Profiles...)
			doc.Overrides = append(doc.Overrides, c.Overrides...)
			if c.Settings != nil {
				doc.Settings = *c.Settings
			}
		case strings.HasPrefix(clean, fileSites) && strings.HasSuffix(clean, ".yaml"):
			var s exportSite
			if err := fromYAML(files[name], &s); err != nil {
				return Document{}, problem("bad_config", p, err.Error(), "")
			}
			doc.Sites = append(doc.Sites, s.Site)
			if s.Cluster != nil {
				doc.Clusters = append(doc.Clusters, *s.Cluster)
			}
			doc.HostPools = append(doc.HostPools, s.HostPools...)
		case strings.HasPrefix(clean, fileStacks) && strings.HasSuffix(clean, ".yaml"):
			var s Stack
			if err := fromYAML(files[name], &s); err != nil {
				return Document{}, problem("bad_config", p, err.Error(), "")
			}
			doc.Stacks = append(doc.Stacks, s)
		case clean == filePlacements:
			var pl exportPlacements
			if err := fromYAML(files[name], &pl); err != nil {
				return Document{}, problem("bad_config", p, err.Error(), "")
			}
			doc.Placements = append(doc.Placements, pl.Placements...)
		default:
			return Document{}, problem("bad_config", p, "not a file of a configuration export", "use config.yaml, sites/<id>.yaml, stacks/<id>.yaml and placements.yaml")
		}
	}
	doc = doc.Normalized()
	if ps := SecretProblems(doc); len(ps) > 0 {
		return Document{}, ps
	}
	return doc, nil
}

// toYAML writes a value as YAML with its JSON field names, leaving out
// what JSON leaves out.
func toYAML(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var plain any
	if err := json.Unmarshal(raw, &plain); err != nil {
		return "", err
	}
	out, err := yaml.Marshal(plain)
	return string(out), err
}

// fromYAML reads YAML into a value through JSON, so JSON field names and
// rules apply; unknown fields are refused.
func fromYAML(s string, into any) error {
	var plain any
	if err := yaml.Unmarshal([]byte(s), &plain); err != nil {
		return err
	}
	raw, err := json.Marshal(plain)
	if err != nil {
		return err
	}
	if string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

var (
	jwtLike  = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)
	natsSeed = regexp.MustCompile(`\bS[UAONCPX][A-Z2-7]{55}\b`)
	pemKey   = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	// secretMarks are the placeholders a stored secret leaves in a value.
	secretMarks = map[string]bool{"$secret": true, "$ref": true, "$vault": true}
)

// SecretProblems names, by path, every value of a document that is or
// looks like a secret: a stored secret's placeholder, a JWT, a NATS seed
// or a PEM private key. A configuration names connections and secrets,
// never holds them.
func SecretProblems(doc Document) []live.Problem {
	raw, _ := json.Marshal(doc)
	var plain any
	_ = json.Unmarshal(raw, &plain)
	var out []live.Problem
	var walk func(p string, v any)
	walk = func(p string, v any) {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if secretMarks[k] {
					out = append(out, live.Problem{Code: "secret_in_config", Path: p, Message: "a stored secret's placeholder (" + k + ") is in the configuration", Fix: "name the connection or secret that holds it instead"})
					continue
				}
				walk(join(p, k), x[k])
			}
		case []any:
			for i, it := range x {
				key := fmt.Sprint(i)
				if m, ok := it.(map[string]any); ok {
					for _, f := range []string{"id", "name", "path", "site"} {
						if s, ok := m[f].(string); ok && s != "" {
							key = s
							break
						}
					}
				}
				walk(p+"["+key+"]", it)
			}
		case string:
			what := ""
			switch {
			case pemKey.MatchString(x):
				what = "a PEM private key"
			case natsSeed.MatchString(x):
				what = "a NATS seed"
			case jwtLike.MatchString(x):
				what = "a JWT"
			}
			if what != "" {
				out = append(out, live.Problem{Code: "secret_in_config", Path: p, Message: "this looks like " + what, Fix: "keep it in a connection or a secret and name that instead"})
			}
		}
	}
	walk("document", plain)
	return out
}

func join(p, k string) string {
	if p == "" {
		return k
	}
	return p + "." + k
}

func contributeExport(reg *extensions.Registry) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.config.export", Source: source}, Title: "Export a configuration",
		Description: "A configuration version as YAML files {files: {path: text}}: config.yaml (accounts, profiles, tenant overrides, settings), sites/<id>.yaml (a site with its cluster and host pools), stacks/<id>.yaml and placements.yaml, to keep in a repository and read back with infra.config.import {files}. version 0 or left out is the draft, which it follows as it changes. Refused (secret_in_config, with the path) when a value is or looks like a secret.",
		Params:      json.RawMessage(`{"type":"object","properties":{"config":{"type":"string","description":"The configuration, main when left out"},"version":{"type":"integer","minimum":0,"description":"The version; 0 or left out for the draft"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"version":2}`)},
		Action:      ActionRead,
		Run:         runExport,
	})
}

func runExport(f live.Feed) error {
	var p struct {
		Config  string `json:"config"`
		Version int    `json:"version"`
	}
	_ = json.Unmarshal(f.Params(), &p)
	if p.Config == "" || !live.ValidToken(p.Config) {
		p.Config = DefaultConfig
	}
	tenant := f.Args().Principal.TenantID
	exported := func(doc Document) (any, error) {
		files, ps := Export(doc)
		if len(ps) > 0 {
			return nil, fmt.Errorf("%s: %s", ps[0].Path, ps[0].Message)
		}
		return map[string]any{"files": files}, nil
	}
	if p.Version > 0 {
		v, ok, err := f.Store().Get(f.Context(), tenant, VersionProjector, VersionType+"."+VersionID(p.Config, p.Version))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("there is no version %d", p.Version)
		}
		var ver Version
		if err := json.Unmarshal(v.Value, &ver); err != nil {
			return err
		}
		out, err := exported(ver.Document)
		if err != nil {
			return err
		}
		f.Snapshot(out, v.Position())
		<-f.Context().Done()
		return nil
	}
	entries, err := f.Store().Watch(f.Context(), tenant, ConfigProjector, ConfigType+"."+p.Config)
	if err != nil {
		return err
	}
	cfg := Config{Draft: Document{}.Normalized()}
	pos := live.Position{}
	started := false
	send := func() error {
		out, err := exported(cfg.Draft)
		if err != nil {
			return err
		}
		if !started {
			f.Snapshot(out, pos)
			started = true
		} else {
			f.Change(live.Delta{Op: "replace", Value: mustJSON(out)}, pos)
		}
		return nil
	}
	for e := range entries {
		if e == nil {
			if err := send(); err != nil {
				return err
			}
			continue
		}
		pos = pos.Merge(e.Value.Position())
		if !e.Deleted {
			_ = json.Unmarshal(e.Value.Value, &cfg)
		}
		if started {
			if err := send(); err != nil {
				return err
			}
		}
	}
	return nil
}
