package installs

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// SettingsQuery is an installed app's settings as its reader sees them,
// what a page's app.settings binding holds.
const SettingsQuery = "app.settings"

// Section is the configuration section an installed app's settings are
// (forge 05c): app.<appId>.
func Section(app string) string { return "app." + app }

// sectionOf is an installed app's settings section: its manifest's schema
// and defaults, set per tenant and per user, under Settings › Apps.
func sectionOf(in apps.Install) (config.Section, bool) {
	m := in.Version.Manifest
	if m.Settings.Schema == nil {
		return config.Section{}, false
	}
	schema, _ := json.Marshal(m.Settings.Schema)
	defaults, _ := json.Marshal(m.Settings.Defaults)
	if m.Settings.Defaults == nil {
		defaults = []byte(`{}`)
	}
	return config.Section{
		Descriptor: extensions.Descriptor{ID: Section(in.App), Source: "app:" + in.App},
		Title:      m.Name, Description: "The settings of the app " + m.Name,
		Category: []string{"Apps", m.Name}, Scopes: []string{"tenant", "user"},
		Schema: schema, Defaults: defaults, Version: in.Version.Version,
	}, true
}

// contributeSettings registers the apps' sections, the app.settings query
// and the App setting node.
func contributeSettings(reg *extensions.Registry, deps func() Deps) {
	config.SectionSources.Register(reg, config.SectionSource{
		Descriptor: extensions.Descriptor{ID: "apps", Source: "apps"},
		Sections: func(_ context.Context, tenant uint64) []config.Section {
			installed := cacheOf(deps().Projections).installed(tenant)
			ids := make([]string, 0, len(installed))
			for id := range installed {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			var out []config.Section
			for _, id := range ids {
				if s, ok := sectionOf(installed[id]); ok {
					out = append(out, s)
				}
			}
			return out
		},
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: SettingsQuery, Source: "apps"}, Title: "An app's settings",
		Description: "An installed app's settings as the reader sees them (its defaults, under the tenant's, under the user's own), the configuration section app.<appId>; the whole value is replaced when a setting or the app changes. Secret settings are left out.",
		Params:      json.RawMessage(`{"type":"object","properties":{"app":{"type":"string","description":"The app's id"}},"required":["app"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"app":"tasks"}`)},
		Action:      "config.read",
		Resource: func(p features.Principal, params json.RawMessage) string {
			var a struct{ App string }
			_ = json.Unmarshal(params, &a)
			return authz.Tenant(p) + "/config:" + Section(a.App)
		},
		Run: func(f live.Feed) error {
			var a struct{ App string }
			_ = json.Unmarshal(f.Params(), &a)
			d := deps()
			args := f.Args()
			installs, err := f.Store().Watch(f.Context(), args.Principal.TenantID, Projector, TargetType+"."+a.App)
			if err != nil {
				return err
			}
			watch := d.Config.Watch(configservice.WatchArgs{ServiceArgs: args})
			sent := false
			send := func() {
				value := Settings(args, d, a.App)
				if !sent {
					sent = true
					f.Snapshot(value, live.Position{})
					return
				}
				raw, _ := json.Marshal(value)
				f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
			}
			started := false
			for {
				select {
				case <-f.Context().Done():
					return nil
				case e, ok := <-installs:
					if !ok {
						return nil
					}
					if e == nil {
						started = true
						send()
					} else if started {
						send()
					}
				case c, ok := <-watch.Events:
					if !ok {
						return nil
					}
					if started && c.Section == Section(a.App) {
						send()
					}
				}
			}
		},
	})

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "apps.setting", Source: "apps", Order: 570}, Title: "App setting", Category: "Data",
		Description: "Reads an installed app's settings as the run's user sees them: the app's defaults, under the tenant's, under the user's own (the section app.<appId>). With a key, the one setting; without, them all.",
		Example:     "App setting tasks / defaultDueDays, added to Now for a new task's due date",
		Config:      json.RawMessage(`{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"key":{"type":"string","description":"A setting's key, dotted for a nested one; empty for every setting"}},"required":["app"]}`),
		Pins: cognition.Pins{Out: []cognition.Pin{
			cognition.DataPin("value", "Value", "json", "The setting's value (every setting without a key); null when the app is not installed")}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			var c struct{ App, Key string }
			_ = json.Unmarshal(x.Node.Config, &c)
			var value any = Settings(x.ServiceArgs, deps(), c.App)
			if value.(map[string]any) == nil {
				return cognition.Values{"value": nil}, "", nil
			}
			if c.Key != "" {
				value, _ = config.Get(value.(map[string]any), c.Key)
			}
			return cognition.Values{"value": value}, "", nil
		},
	})
}

// Settings are an installed app's settings as args' principal sees them,
// without its secret ones; nil when it is not installed or has none.
func Settings(args features.ServiceArgs, d Deps, app string) map[string]any {
	if d.Config == nil {
		return nil
	}
	in, ok := Running(args.Context(), d.Projections, args.Principal.TenantID, app)
	if !ok {
		return nil
	}
	sec, ok := sectionOf(in)
	if !ok {
		return nil
	}
	raw, err := d.Config.Resolved(args, sec.ID)
	var value map[string]any
	if err != nil || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	if schema, err := config.ParseSchema(sec.Schema); err == nil {
		schema.Leaves(func(path string, leaf *config.Schema) {
			if leaf.Type == "secret" {
				config.Delete(value, path)
			}
		})
	}
	return value
}
