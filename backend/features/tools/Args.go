package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// schema is the part of JSON Schema tool inputs are checked against: types,
// required properties, enums, and string and array lengths. Anything else
// in a tool's Input is passed to the model but not checked.
type schema struct {
	Type       string             `json:"type"`
	Properties map[string]*schema `json:"properties"`
	Required   []string           `json:"required"`
	Items      *schema            `json:"items"`
	Enum       []any              `json:"enum"`
	MinLength  *int               `json:"minLength"`
	MaxLength  *int               `json:"maxLength"`
	MinItems   *int               `json:"minItems"`
	MaxItems   *int               `json:"maxItems"`
	Minimum    *float64           `json:"minimum"`
	Maximum    *float64           `json:"maximum"`
}

// CheckArgs reports the first way args fails the input schema, phrased for
// the model to correct its call; nil when it matches.
func CheckArgs(input, args json.RawMessage) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return fmt.Errorf("the arguments are not valid JSON: %v", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return fmt.Errorf("the arguments must be a JSON object")
	}
	if len(input) == 0 {
		return nil
	}
	var s schema
	if err := json.Unmarshal(input, &s); err != nil {
		return fmt.Errorf("the tool's input schema is invalid: %v", err)
	}
	return s.check("", v)
}

func (s *schema) check(path string, v any) error {
	name := path
	if name == "" {
		name = "the arguments"
	}
	if len(s.Enum) > 0 && !slices.ContainsFunc(s.Enum, func(e any) bool { return fmt.Sprint(e) == fmt.Sprint(v) }) {
		return fmt.Errorf("%s must be one of %v", name, s.Enum)
	}
	switch s.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", name)
		}
		for _, r := range s.Required {
			if _, ok := obj[r]; !ok {
				return fmt.Errorf("%s is required", join(path, r))
			}
		}
		for k, val := range obj {
			if p, ok := s.Properties[k]; ok && val != nil {
				if err := p.check(join(path, k), val); err != nil {
					return err
				}
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", name)
		}
		if s.MinItems != nil && len(arr) < *s.MinItems {
			return fmt.Errorf("%s needs at least %d items", name, *s.MinItems)
		}
		if s.MaxItems != nil && len(arr) > *s.MaxItems {
			return fmt.Errorf("%s takes at most %d items", name, *s.MaxItems)
		}
		if s.Items != nil {
			for i, item := range arr {
				if err := s.Items.check(fmt.Sprintf("%s[%d]", name, i), item); err != nil {
					return err
				}
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", name)
		}
		if s.MinLength != nil && len([]rune(str)) < *s.MinLength {
			return fmt.Errorf("%s must be at least %d characters", name, *s.MinLength)
		}
		if s.MaxLength != nil && len([]rune(str)) > *s.MaxLength {
			return fmt.Errorf("%s must be at most %d characters", name, *s.MaxLength)
		}
	case "integer", "number":
		n, ok := v.(float64)
		if !ok {
			return fmt.Errorf("%s must be a number", name)
		}
		if s.Type == "integer" && n != math.Trunc(n) {
			return fmt.Errorf("%s must be a whole number", name)
		}
		if s.Minimum != nil && n < *s.Minimum {
			return fmt.Errorf("%s must be at least %v", name, *s.Minimum)
		}
		if s.Maximum != nil && n > *s.Maximum {
			return fmt.Errorf("%s must be at most %v", name, *s.Maximum)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false", name)
		}
	}
	return nil
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// Decode reads a call's arguments into T, after CheckArgs has passed them.
func Decode[T any](call Call) (T, error) {
	var out T
	args := call.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, &out); err != nil {
		return out, fmt.Errorf("the arguments do not fit %s: %v", call.Name, err)
	}
	return out, nil
}

// wireName is a tool's name as the model sees it: providers allow letters,
// digits, '_' and '-'.
func wireName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}
