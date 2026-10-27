package extensions

import "testing"

type widget struct {
	Descriptor
	Label string
}

var widgets = NewPoint[widget]("test.widget", "test widgets")

func TestRegisterAndAll(t *testing.T) {
	reg := NewRegistry()
	widgets.Register(reg, widget{Descriptor: Descriptor{ID: "b", Source: "x"}, Label: "B"})
	widgets.Register(reg, widget{Descriptor: Descriptor{ID: "a", Order: 2000}, Label: "A"})
	widgets.Register(reg, widget{Descriptor: Descriptor{ID: "c", Order: 10}, Label: "C"})
	widgets.Register(reg, widget{Descriptor: Descriptor{ID: "b"}, Label: "B2"}) // replaces b

	all := widgets.All(reg)
	var labels []string
	for _, w := range all {
		labels = append(labels, w.Label)
	}
	if got := len(labels); got != 3 || labels[0] != "C" || labels[1] != "B2" || labels[2] != "A" {
		t.Errorf("labels = %v, want [C B2 A]", labels)
	}
}

func TestPointsListsDeclaredPoints(t *testing.T) {
	reg := NewRegistry()
	other := NewPoint[widget]("test.other", "declared, nothing contributed")
	reg.Declare(widgets, other)
	widgets.Register(reg, widget{Descriptor: Descriptor{ID: "a", Source: "x"}})

	points := reg.Points()
	if len(points) != 2 || points[0].ID != "test.widget" || points[1].ID != "test.other" {
		t.Fatalf("points = %+v", points)
	}
	if c := points[0].Contributions; len(c) != 1 || c[0].Order != DefaultOrder || c[0].Source != "x" {
		t.Errorf("contributions = %+v", c)
	}
	if points[1].Contributions == nil || len(points[1].Contributions) != 0 {
		t.Errorf("empty point = %+v", points[1])
	}
	if other.All(reg) != nil && len(other.All(reg)) != 0 {
		t.Error("an empty point has contributions")
	}
}
