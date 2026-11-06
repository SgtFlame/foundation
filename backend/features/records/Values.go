package records

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// IsNull reports whether a raw value is absent or JSON null.
func IsNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null"
}

// CheckValue checks a value against its field and returns it in its
// stored form: a decimal as a string with the field's scale, a dateTime in
// UTC, an enum as its option's id (a name is accepted too). Null is
// returned as nil: the value is cleared. The error's text names nothing
// but the value and the field, so it can be a problem's message.
func CheckValue(f Field, raw json.RawMessage) (json.RawMessage, error) {
	if IsNull(raw) {
		return nil, nil
	}
	t, ok := FieldTypeOf(f.Type)
	if !ok {
		return nil, fmt.Errorf("there is no field type %q", f.Type)
	}
	if t.Computed {
		return nil, fmt.Errorf("%s is computed and cannot be set", f.Name)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("not valid JSON: %v", err)
	}
	out, err := checkValue(f, v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func checkValue(f Field, v any) (any, error) {
	switch f.Type {
	case "text", "longText":
		s, ok := v.(string)
		if !ok {
			return nil, want("text", v)
		}
		return s, nil
	case "richText":
		return checkRichText(v)
	case "file":
		return checkFile(v)
	case "volumeFile":
		return checkVolumeFile(v)
	case "number":
		n, ok := v.(float64)
		if !ok || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, want("a number", v)
		}
		return n, nil
	case "decimal":
		return decimal(v, scaleOf(f))
	case "money":
		if f.Currency != "" {
			return decimal(v, scaleOf(f))
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, want(`{"amount": "12.50", "currency": "USD"}`, v)
		}
		amount, err := decimal(m["amount"], scaleOf(f))
		if err != nil {
			return nil, fmt.Errorf("amount: %w", err)
		}
		c, _ := m["currency"].(string)
		if !currency.MatchString(c) {
			return nil, fmt.Errorf("%q is not an ISO 4217 currency code", c)
		}
		return map[string]any{"amount": amount, "currency": c}, nil
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, want("true or false", v)
		}
		return b, nil
	case "date":
		s, ok := v.(string)
		if _, err := time.Parse(time.DateOnly, s); !ok || err != nil {
			return nil, want("a date, YYYY-MM-DD", v)
		}
		return s, nil
	case "dateTime":
		s, _ := v.(string)
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, want("an RFC 3339 time, e.g. 2026-11-01T09:30:00Z", v)
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case "duration":
		s, _ := v.(string)
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, want(`a duration such as "1h30m"`, v)
		}
		return d.String(), nil
	case "enum":
		s, _ := v.(string)
		if id, ok := optionID(f, s); ok {
			return id, nil
		}
		return nil, fmt.Errorf("%s is not one of %s", show(v), optionList(f))
	case "multiEnum":
		items, ok := v.([]any)
		if !ok {
			return nil, want("a list of options", v)
		}
		out := []string{}
		for _, it := range items {
			s, _ := it.(string)
			id, ok := optionID(f, s)
			if !ok {
				return nil, fmt.Errorf("%s is not one of %s", show(it), optionList(f))
			}
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out, nil
	case "tags":
		items, ok := v.([]any)
		if !ok {
			return nil, want("a list of tags", v)
		}
		out := []string{}
		for _, it := range items {
			s, ok := it.(string)
			if s = strings.TrimSpace(s); !ok || s == "" {
				return nil, want("a tag", it)
			}
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		return out, nil
	case "relation":
		if f.Relation != nil && f.Relation.Many {
			items, ok := v.([]any)
			if !ok {
				return nil, want("a list of record ids", v)
			}
			out := []string{}
			for _, it := range items {
				id, err := recordID(it)
				if err != nil {
					return nil, err
				}
				if !slices.Contains(out, id) {
					out = append(out, id)
				}
			}
			return out, nil
		}
		return recordID(v)
	case "user":
		s, _ := v.(string)
		if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return nil, want("a user id (digits, as a string)", v)
		}
		return s, nil
	case "json":
		if f.Schema != nil {
			if _, ok := v.(map[string]any); ok {
				raw, _ := json.Marshal(v)
				if err := tools.CheckArgs(f.Schema, raw); err != nil {
					return nil, err
				}
			}
		}
		return v, nil
	case "list":
		items, ok := v.([]any)
		if !ok || f.Items == nil {
			return nil, want("a list", v)
		}
		out := make([]any, 0, len(items))
		for i, it := range items {
			c, err := checkValue(*f.Items, it)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			out = append(out, c)
		}
		return out, nil
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return nil, want("an object", v)
		}
		out := map[string]any{}
		for k, x := range m {
			nf, _, ok := EntityType{Fields: f.Fields}.FieldByID(k)
			if !ok || nf.Hidden {
				return nil, fmt.Errorf("%s has no field %q", f.Name, k)
			}
			if x == nil {
				continue
			}
			c, err := checkValue(nf, x)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = c
		}
		for _, nf := range f.Fields {
			if nf.Required && !nf.Hidden && out[nf.ID] == nil {
				return nil, fmt.Errorf("%s needs %s", f.Name, nf.ID)
			}
		}
		return out, nil
	}
	return v, nil
}

// Convert turns a value of field from into one of field to, when a type
// changes: a value to already accepts is kept, the others are read as
// text, number or a list and checked again. The error says why it cannot.
func Convert(from, to Field, raw json.RawMessage) (json.RawMessage, error) {
	if IsNull(raw) {
		return nil, nil
	}
	if out, err := CheckValue(to, raw); err == nil {
		return out, nil
	}
	if t, _ := FieldTypeOf(to.Type); t.Computed {
		return nil, fmt.Errorf("a computed field keeps no values")
	}
	if to.Type == "json" {
		return raw, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if to.Type == "richText" {
		return CheckValue(to, must(json.Marshal(DocFromText(textOf(from, v)))))
	}
	if item, many := ItemOf(to); many {
		var items []any
		switch x := v.(type) {
		case []any:
			items = x
		case string:
			for _, s := range strings.Split(x, ",") {
				if s = strings.TrimSpace(s); s != "" {
					items = append(items, s)
				}
			}
		default:
			items = []any{x}
		}
		fromItem, _ := ItemOf(from)
		out := []json.RawMessage{}
		for i, it := range items {
			raw, _ := json.Marshal(it)
			c, err := Convert(fromItem, item, raw)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			out = append(out, c)
		}
		return CheckValue(to, must(json.Marshal(out)))
	}
	if items, ok := v.([]any); ok {
		if len(items) != 1 {
			return nil, fmt.Errorf("%d values do not fit in one %s", len(items), to.Type)
		}
		fromItem, _ := ItemOf(from)
		return Convert(fromItem, to, must(json.Marshal(items[0])))
	}
	text := textOf(from, v)
	var c any = text
	switch to.Type {
	case "number":
		n, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", text)
		}
		c = n
	case "bool":
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true", "yes", "y", "1", "on":
			c = true
		case "false", "no", "n", "0", "off", "":
			c = false
		default:
			return nil, fmt.Errorf("%q is not true or false", text)
		}
	case "date":
		if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
			c = t.UTC().Format(time.DateOnly)
		}
	case "dateTime":
		if t, err := time.Parse(time.DateOnly, text); err == nil {
			c = t.Format(time.RFC3339)
		}
	case "money":
		if to.Currency == "" {
			if m, ok := v.(map[string]any); ok {
				c = m
			} else {
				return nil, fmt.Errorf("an amount without a currency cannot become money of any currency")
			}
		} else if m, ok := v.(map[string]any); ok {
			if m["currency"] != to.Currency {
				return nil, fmt.Errorf("%v is not in %s", m["currency"], to.Currency)
			}
			c = m["amount"]
		}
	}
	return CheckValue(to, must(json.Marshal(c)))
}

// ItemOf is the field each item of a list-like field is, and whether it is
// one.
func ItemOf(f Field) (Field, bool) {
	switch f.Type {
	case "multiEnum":
		return Field{ID: f.ID, Name: f.Name, Type: "enum", Options: f.Options}, true
	case "tags":
		return Field{ID: f.ID, Name: f.Name, Type: "text"}, true
	case "list":
		if f.Items != nil {
			return *f.Items, true
		}
	case "relation":
		if f.Relation != nil && f.Relation.Many {
			r := *f.Relation
			r.Many = false
			return Field{ID: f.ID, Name: f.Name, Type: "relation", Relation: &r}, true
		}
	}
	return f, false
}

// textOf is a value as text: an option's name, a number without trailing
// zeros, money with its currency, rich text's text, a file's name,
// anything else structured as JSON.
func textOf(f Field, v any) string {
	switch x := v.(type) {
	case string:
		if f.Type == "enum" {
			for _, o := range f.Options {
				if o.ID == x {
					return o.Name
				}
			}
		}
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case map[string]any:
		switch f.Type {
		case "money":
			return fmt.Sprintf("%v %v", x["amount"], x["currency"])
		case "richText":
			return PlainText(must(json.Marshal(x)))
		case "file", "volumeFile":
			if s, ok := x["name"].(string); ok {
				return s
			}
			if s, ok := x["path"].(string); ok {
				return s
			}
		}
	}
	return string(must(json.Marshal(v)))
}

func scaleOf(f Field) int {
	if f.Scale == nil {
		return 2
	}
	return *f.Scale
}

// decimal is an exact number, sent as a string or a JSON number, as a
// string with scale digits after the point.
func decimal(v any, scale int) (string, error) {
	var s string
	switch x := v.(type) {
	case string:
		s = strings.TrimSpace(x)
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return "", want(`a decimal such as "12.50"`, v)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || strings.ContainsAny(s, "/eE") {
		return "", want(`a decimal such as "12.50"`, v)
	}
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)))
	if !scaled.IsInt() {
		return "", fmt.Errorf("%s has more than %d digits after the point", s, scale)
	}
	return r.FloatString(scale), nil
}

func optionID(f Field, s string) (string, bool) {
	for _, o := range f.Options {
		if o.ID == s {
			return o.ID, true
		}
	}
	for _, o := range f.Options {
		if strings.EqualFold(o.Name, strings.TrimSpace(s)) {
			return o.ID, true
		}
	}
	return "", false
}

func optionList(f Field) string {
	var ids []string
	for _, o := range f.Options {
		ids = append(ids, o.ID)
	}
	return strings.Join(ids, ", ")
}

func recordID(v any) (string, error) {
	s, _ := v.(string)
	if !live.ValidToken(s) {
		return "", want("a record id", v)
	}
	return s, nil
}

func want(what string, v any) error {
	return fmt.Errorf("%s is not %s", show(v), what)
}

func show(v any) string {
	raw, _ := json.Marshal(v)
	if len(raw) > 60 {
		raw = append(raw[:57], "..."...)
	}
	return string(raw)
}

func must(raw []byte, _ error) []byte { return raw }
