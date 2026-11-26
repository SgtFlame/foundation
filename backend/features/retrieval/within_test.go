package retrieval

import (
	"slices"
	"testing"
)

// Within and Narrow never widen collections (forge 18f).
func TestWithinNeverWidens(t *testing.T) {
	for _, c := range []struct{ have, want, out []string }{
		{nil, nil, nil},
		{nil, []string{"a"}, []string{"a"}},
		{[]string{"a", "b"}, nil, []string{"a", "b"}},
		{[]string{"a", "b"}, []string{"b", "c"}, []string{"b"}},
		{[]string{"a"}, []string{"c"}, []string{"none"}},
	} {
		if got := Within(c.have, c.want); !slices.Equal(got, c.out) {
			t.Errorf("Within(%v, %v) = %v", c.have, c.want, got)
		}
	}
	o := Options{Collections: []string{"s1"}}.Narrow(Options{Collections: []string{"s2"}, Partitions: []string{"chat-x"}})
	if !slices.Equal(o.Collections, []string{"none"}) || !slices.Equal(o.Partitions, []string{"chat-x"}) {
		t.Errorf("narrowed %+v", o)
	}
	p, ok, err := ParseOptions(`{"sources":["blackboards"],"collections":["s1"],"partitions":["main"]}`)
	if err != nil || !ok || !slices.Equal(p.Collections, []string{"s1"}) || !slices.Equal(p.Partitions, []string{"main"}) {
		t.Errorf("parsed %+v %v %v", p, ok, err)
	}
}
