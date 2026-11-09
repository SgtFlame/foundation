// Package tmpl renders the Go text/template templates graphs build prompts
// and tools with (forge 06i): a Template node's text, a tool's description,
// Chronicle's prompts. Templates see their data as the dot, and a small set
// of functions besides Go's own: text (join, upper, lower, trim, indent,
// clip, oneLine, replace, split, contains, hasPrefix, hasSuffix), values
// (default, json, list, dict, tail, add, sub), now, and setting, which reads
// a configuration section as the run's principal sees it.
package tmpl

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"text/template/parse"
	"time"
	"unicode/utf8"
)

// Env is what a template's functions reach outside its data.
type Env struct {
	// Setting reads a path of a configuration section ("builder",
	// "maxSteps"); an empty path is the whole section. Nil makes setting fail.
	Setting func(section, path string) (any, error)
}

// Funcs are the functions every template may call, for env.
func Funcs(env Env) template.FuncMap {
	return template.FuncMap{
		"join": func(sep string, list any) string {
			var parts []string
			for _, v := range items(list) {
				parts = append(parts, text(v))
			}
			return strings.Join(parts, sep)
		},
		"upper":     strings.ToUpper,
		"lower":     strings.ToLower,
		"trim":      strings.TrimSpace,
		"replace":   func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
		"split":     func(sep, s string) []string { return strings.Split(s, sep) },
		"contains":  func(sub, s string) bool { return strings.Contains(s, sub) },
		"hasPrefix": func(prefix, s string) bool { return strings.HasPrefix(s, prefix) },
		"hasSuffix": func(suffix, s string) bool { return strings.HasSuffix(s, suffix) },
		"oneLine":   func(s string) string { return strings.Join(strings.Fields(s), " ") },
		"indent": func(n int, s string) string {
			pad := strings.Repeat(" ", n)
			return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
		},
		"clip": Clip,
		"default": func(def, v any) any {
			if empty(v) {
				return def
			}
			return v
		},
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"list": func(v ...any) []any { return v },
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, fmt.Errorf("dict takes keys and values in pairs")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				m[text(kv[i])] = kv[i+1]
			}
			return m, nil
		},
		"tail": func(n int, list any) []any {
			all := items(list)
			return all[max(0, len(all)-n):]
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"now": func() string { return time.Now().UTC().Format(time.RFC3339) },
		"setting": func(section string, path ...string) (any, error) {
			if env.Setting == nil {
				return nil, fmt.Errorf("settings cannot be read here")
			}
			return env.Setting(section, strings.Join(path, "."))
		},
	}
}

// Parse parses a template with the functions of env; a missing key renders
// as nothing rather than "<no value>".
func Parse(name, text string, env Env) (*template.Template, error) {
	return template.New(name).Funcs(Funcs(env)).Option("missingkey=zero").Parse(text)
}

// Render renders text with data.
func Render(text string, data any, env Env) (string, error) {
	t, err := Parse("template", text, env)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return strings.ReplaceAll(b.String(), "<no value>", ""), nil
}

// Fields are the names a template reads from its data at the top: .name
// outside range and with, and $.name anywhere, in the order they first
// appear. A Template node makes an input pin of each.
func Fields(text string) ([]string, error) {
	t, err := Parse("template", text, Env{})
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	var walk func(n parse.Node, top bool)
	walk = func(n parse.Node, top bool) {
		switch n := n.(type) {
		case nil:
		case *parse.ListNode:
			if n != nil {
				for _, c := range n.Nodes {
					walk(c, top)
				}
			}
		case *parse.ActionNode:
			walk(n.Pipe, top)
		case *parse.PipeNode:
			if n == nil {
				return
			}
			for _, c := range n.Cmds {
				walk(c, top)
			}
		case *parse.CommandNode:
			for _, a := range n.Args {
				walk(a, top)
			}
		case *parse.FieldNode:
			if top && len(n.Ident) > 0 {
				add(n.Ident[0])
			}
		case *parse.VariableNode:
			if len(n.Ident) > 1 && n.Ident[0] == "$" {
				add(n.Ident[1])
			}
		case *parse.ChainNode:
			walk(n.Node, top)
		case *parse.IfNode:
			walk(n.Pipe, top)
			walk(n.List, top)
			walk(n.ElseList, top)
		case *parse.RangeNode:
			walk(n.Pipe, top)
			walk(n.List, false)
			walk(n.ElseList, top)
		case *parse.WithNode:
			walk(n.Pipe, top)
			walk(n.List, false)
			walk(n.ElseList, top)
		case *parse.TemplateNode:
			walk(n.Pipe, top)
		}
	}
	for _, tt := range t.Templates() {
		if tt.Tree != nil {
			walk(tt.Tree.Root, true)
		}
	}
	return out, nil
}

// Clip cuts s to about n bytes on a rune boundary, marking the cut.
func Clip(n int, s string) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// items are a list's elements; anything else is a list of itself.
func items(list any) []any {
	if list == nil {
		return nil
	}
	v := reflect.ValueOf(list)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return []any{list}
	}
	out := make([]any, v.Len())
	for i := range out {
		out[i] = v.Index(i).Interface()
	}
	return out
}

func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	return fmt.Sprint(v)
}

func empty(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return r.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return r.IsNil()
	}
	return r.IsZero()
}
