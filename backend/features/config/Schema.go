package config

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Schema is the JSON-Schema subset sections are declared in: enough to
// render a form and to validate a value on the server.
type Schema struct {
	Type        string             `json:"type"` // string, number, integer, boolean, enum, secret, object or array
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"` // object
	Required    []string           `json:"required,omitempty"`   // object
	Items       *Schema            `json:"items,omitempty"`      // array
	Key         string             `json:"key,omitempty"`        // array of objects: the property unique per item, and what a ref selects ("id" when empty)
	Options     []EnumOption       `json:"options,omitempty"`    // enum
	Min         *float64           `json:"min,omitempty"`
	Max         *float64           `json:"max,omitempty"`
	Step        *float64           `json:"step,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`
	Format      string             `json:"format,omitempty"` // url, multiline, currency, duration
	Unit        string             `json:"unit,omitempty"`
	Scopes      []string           `json:"scopes,omitempty"` // overrides the section's scopes for this key
	Restart     bool               `json:"restart,omitempty"`
	Ref         string             `json:"ref,omitempty"` // "<sectionId>#<arrayPath>": a string naming an item of another section's array by its key
}

type EnumOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// FieldError is a validation failure at a dotted path, array items by index
// ("models.0.pricing.inputPerMTok"); the empty path is the whole value.
type FieldError struct {
	Path    string
	Message string
}

// ParseSchema decodes a section's schema.
func ParseSchema(raw json.RawMessage) (*Schema, error) {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.Type != "object" {
		return nil, fmt.Errorf("a section schema must be an object, not %q", s.Type)
	}
	return &s, nil
}

// Join appends a segment to a dotted path.
func Join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// Validate checks a decoded JSON value (map[string]any, []any, float64,
// string, bool) against s. Secrets are checked in their plain form.
func (s *Schema) Validate(value any) []FieldError {
	var errs []FieldError
	s.validate("", value, &errs)
	return errs
}

func (s *Schema) validate(path string, value any, errs *[]FieldError) {
	fail := func(format string, args ...any) {
		*errs = append(*errs, FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	switch s.Type {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			fail("must be an object")
			return
		}
		for _, k := range s.Required {
			if v, ok := obj[k]; !ok || v == nil {
				*errs = append(*errs, FieldError{Path: Join(path, k), Message: "is required"})
			}
		}
		for _, k := range sortedKeys(obj) {
			child, ok := s.Properties[k]
			if !ok {
				*errs = append(*errs, FieldError{Path: Join(path, k), Message: "is not a known setting"})
				continue
			}
			if obj[k] != nil {
				child.validate(Join(path, k), obj[k], errs)
			}
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			fail("must be a list")
			return
		}
		if s.Min != nil && float64(len(arr)) < *s.Min {
			fail("needs at least %v items", *s.Min)
		}
		if s.Max != nil && float64(len(arr)) > *s.Max {
			fail("allows at most %v items", *s.Max)
		}
		seen := map[string]int{}
		for i, item := range arr {
			p := Join(path, strconv.Itoa(i))
			if s.Items != nil {
				s.Items.validate(p, item, errs)
			}
			if s.Items != nil && s.Items.Type == "object" {
				key := s.KeyName()
				if obj, ok := item.(map[string]any); ok {
					if k, ok := obj[key].(string); ok && k != "" {
						if j, dup := seen[k]; dup {
							*errs = append(*errs, FieldError{Path: Join(p, key), Message: fmt.Sprintf("repeats %q from item %d", k, j+1)})
						}
						seen[k] = i
					}
				}
			}
		}
	case "string", "secret":
		str, ok := value.(string)
		if !ok {
			fail("must be text")
			return
		}
		if s.Pattern != "" {
			if re, err := regexp.Compile(s.Pattern); err == nil && !re.MatchString(str) {
				fail("does not match %s", s.Pattern)
			}
		}
		if s.Min != nil && float64(len(str)) < *s.Min {
			fail("needs at least %v characters", *s.Min)
		}
		if s.Max != nil && float64(len(str)) > *s.Max {
			fail("allows at most %v characters", *s.Max)
		}
	case "enum":
		str, ok := value.(string)
		if !ok || !slices.ContainsFunc(s.Options, func(o EnumOption) bool { return o.Value == str }) {
			values := make([]string, len(s.Options))
			for i, o := range s.Options {
				values[i] = o.Value
			}
			fail("must be one of %s", strings.Join(values, ", "))
		}
	case "number", "integer":
		n, ok := value.(float64)
		if !ok {
			fail("must be a number")
			return
		}
		if s.Type == "integer" && n != math.Trunc(n) {
			fail("must be a whole number")
		}
		if s.Min != nil && n < *s.Min {
			fail("must be at least %v", *s.Min)
		}
		if s.Max != nil && n > *s.Max {
			fail("must be at most %v", *s.Max)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			fail("must be true or false")
		}
	default:
		fail("has an unknown type %q in its schema", s.Type)
	}
}

// KeyName is the property that identifies an array's object items.
func (s *Schema) KeyName() string {
	if s.Key != "" {
		return s.Key
	}
	return "id"
}

// At is the schema of the setting at a dotted path, or nil. Array items
// are addressed by index.
func (s *Schema) At(path string) *Schema {
	cur := s
	for _, seg := range splitPath(path) {
		switch {
		case cur == nil:
			return nil
		case cur.Type == "object":
			cur = cur.Properties[seg]
		case cur.Type == "array":
			cur = cur.Items
		default:
			return nil
		}
	}
	return cur
}

// Leaves calls fn for every leaf setting under s: every setting that is not
// an object. Arrays are leaves, edited and inherited as a whole.
func (s *Schema) Leaves(fn func(path string, leaf *Schema)) {
	s.leaves("", fn)
}

func (s *Schema) leaves(path string, fn func(string, *Schema)) {
	if s.Type != "object" {
		fn(path, s)
		return
	}
	for _, k := range sortedKeys(s.Properties) {
		s.Properties[k].leaves(Join(path, k), fn)
	}
}

// AllowsScope is whether the leaf at path takes values at scope (a scope kind:
// app, tenant or user), given the section's own scopes.
func (s *Schema) AllowsScope(path string, sectionScopes []string, scope string) bool {
	scopes := sectionScopes
	cur := s
	for _, seg := range splitPath(path) {
		if cur == nil {
			break
		}
		if cur.Type == "object" {
			cur = cur.Properties[seg]
		} else {
			cur = cur.Items
		}
		if cur != nil && len(cur.Scopes) > 0 {
			scopes = cur.Scopes
		}
	}
	return slices.Contains(scopes, scope)
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
