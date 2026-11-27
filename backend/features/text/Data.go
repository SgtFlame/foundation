package text

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
)

// DataChange is one change between two JSON values (forge 22b): an item
// or field added, removed or changed, or a keyed item moved within its
// list. Path names it as list[key].field (list[3] for an unkeyed list's
// item, by its index).
type DataChange struct {
	Op     string `json:"op"` // added, removed, changed, moved
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

// DataDiff is the changes from a to b, both JSON values (or Go values
// that marshal to JSON). Keys names the lists whose items are matched by
// a field rather than by position: the list's path with [] for list items
// and no keys ("sites", "profiles[].units") to the field ("id", "name").
// Equal values give no changes; map keys are visited in order, so the
// same values always give the same changes.
func DataDiff(a, b any, keys map[string]string) []DataChange {
	var out []DataChange
	dataDiff(&out, "", "", plainJSON(a), plainJSON(b), keys)
	return out
}

func plainJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func join(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

func dataDiff(out *[]DataChange, path, pattern string, a, b any, keys map[string]string) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			break
		}
		names := map[string]bool{}
		for k := range av {
			names[k] = true
		}
		for k := range bv {
			names[k] = true
		}
		sorted := make([]string, 0, len(names))
		for k := range names {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			x, inA := av[k]
			y, inB := bv[k]
			p, pat := join(path, k), join(pattern, k)
			switch {
			case !inA:
				if !isEmpty(y) {
					*out = append(*out, DataChange{Op: "added", Path: p, After: y})
				}
			case !inB:
				if !isEmpty(x) {
					*out = append(*out, DataChange{Op: "removed", Path: p, Before: x})
				}
			default:
				dataDiff(out, p, pat, x, y, keys)
			}
		}
		return
	case []any:
		bv, ok := b.([]any)
		if !ok {
			break
		}
		key := keys[pattern]
		if key == "" {
			if !reflect.DeepEqual(av, bv) {
				n := len(av)
				if len(bv) > n {
					n = len(bv)
				}
				for i := 0; i < n; i++ {
					p := path + "[" + strconv.Itoa(i) + "]"
					switch {
					case i >= len(av):
						*out = append(*out, DataChange{Op: "added", Path: p, After: bv[i]})
					case i >= len(bv):
						*out = append(*out, DataChange{Op: "removed", Path: p, Before: av[i]})
					default:
						dataDiff(out, p, pattern+"[]", av[i], bv[i], keys)
					}
				}
			}
			return
		}
		ka, ia := keyed(av, key)
		kb, ib := keyed(bv, key)
		var common []string
		for _, k := range ka {
			if _, ok := ib[k]; ok {
				common = append(common, k)
			}
		}
		var commonB []string
		for _, k := range kb {
			if _, ok := ia[k]; ok {
				commonB = append(commonB, k)
			}
		}
		for _, k := range ka {
			p := path + "[" + k + "]"
			if _, ok := ib[k]; !ok {
				*out = append(*out, DataChange{Op: "removed", Path: p, Before: av[ia[k]]})
				continue
			}
			dataDiff(out, p, pattern+"[]", av[ia[k]], bv[ib[k]], keys)
		}
		for i, k := range common {
			if commonB[i] != k {
				*out = append(*out, DataChange{Op: "moved", Path: path + "[" + k + "]", Before: indexOf(common, k), After: indexOf(commonB, k)})
			}
		}
		for _, k := range kb {
			if _, ok := ia[k]; !ok {
				*out = append(*out, DataChange{Op: "added", Path: path + "[" + k + "]", After: bv[ib[k]]})
			}
		}
		return
	}
	if !reflect.DeepEqual(a, b) {
		switch {
		case isEmpty(a):
			*out = append(*out, DataChange{Op: "added", Path: path, After: b})
		case isEmpty(b):
			*out = append(*out, DataChange{Op: "removed", Path: path, Before: a})
		default:
			*out = append(*out, DataChange{Op: "changed", Path: path, Before: a, After: b})
		}
	}
}

// isEmpty is a value that says nothing: null, "", false, 0, [] or {}.
func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// keyed is a list's item keys in order and each key's index; an item
// without the key is keyed by its index.
func keyed(list []any, key string) ([]string, map[string]int) {
	order := make([]string, 0, len(list))
	idx := map[string]int{}
	for i, it := range list {
		k := "#" + strconv.Itoa(i)
		if m, ok := it.(map[string]any); ok {
			switch v := m[key].(type) {
			case string:
				k = v
			case float64:
				k = strconv.FormatFloat(v, 'f', -1, 64)
			}
		}
		if _, dup := idx[k]; dup {
			k = k + "#" + strconv.Itoa(i)
		}
		order = append(order, k)
		idx[k] = i
	}
	return order, idx
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
