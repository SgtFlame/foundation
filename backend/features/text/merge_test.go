package text

import "testing"

const mergeBase = "# Refunds\n\nRefunds up to 100 EUR.\n\n# Booking\n\nA party size.\nA date.\n"

func TestMergeOneSide(t *testing.T) {
	src := "# Refunds\n\nRefunds up to 100 EUR.\n\n# Booking\n\nA party size.\nA date.\nA time.\n"
	r := Merge3(mergeBase, src, mergeBase, nil, "")
	if !r.Clean || r.Merged != src || !r.SourceChanged || r.TargetChanged {
		t.Fatalf("one side: %+v", r)
	}
}

func TestMergeBothSidesApart(t *testing.T) {
	src := "# Refunds\n\nRefunds up to 100 EUR, within 14 days.\n\n# Booking\n\nA party size.\nA date.\n"
	tgt := "# Refunds\n\nRefunds up to 100 EUR.\n\n# Booking\n\nA party size.\nA date.\nA phone number.\n"
	r := Merge3(mergeBase, src, tgt, nil, "")
	want := "# Refunds\n\nRefunds up to 100 EUR, within 14 days.\n\n# Booking\n\nA party size.\nA date.\nA phone number.\n"
	if !r.Clean || r.Merged != want {
		t.Fatalf("apart: %q %+v", r.Merged, r.Conflicts)
	}
	// Adjacent lines changed on each side are no conflict.
	src2 := "# Refunds\n\nRefunds up to 100 EUR.\n\n# Booking\n\nA party of 2 to 8.\nA date.\n"
	tgt2 := "# Refunds\n\nRefunds up to 100 EUR.\n\n# Booking\n\nA party size.\nA date in the next 60 days.\n"
	r = Merge3(mergeBase, src2, tgt2, nil, "")
	if !r.Clean || r.Merged != "# Refunds\n\nRefunds up to 100 EUR.\n\n# Booking\n\nA party of 2 to 8.\nA date in the next 60 days.\n" {
		t.Fatalf("adjacent: %q %+v", r.Merged, r.Conflicts)
	}
}

func TestMergeSameChange(t *testing.T) {
	src := "# Refunds\n\nRefunds up to 500 EUR.\n\n# Booking\n\nA party size.\nA date.\n"
	r := Merge3(mergeBase, src, src, nil, "")
	if !r.Clean || r.Merged != src {
		t.Fatalf("same: %+v", r)
	}
}

func TestMergeConflict(t *testing.T) {
	src := "# Refunds\n\nRefunds up to 500 EUR.\n\n# Booking\n\nA party size.\nA date.\nA time.\n"
	tgt := "# Refunds\n\nRefunds up to 1000 EUR.\n\n# Booking\n\nA party size.\nA date.\n"
	r := Merge3(mergeBase, src, tgt, nil, "")
	if r.Clean || len(r.Conflicts) != 1 {
		t.Fatalf("conflict: %+v", r)
	}
	c := r.Conflicts[0]
	if c.Base != "Refunds up to 100 EUR.\n" || c.Source != "Refunds up to 500 EUR.\n" || c.Target != "Refunds up to 1000 EUR.\n" || len(c.HeadingPath) != 1 || c.HeadingPath[0] != "Refunds" || c.BaseLines.Start != 3 {
		t.Fatalf("conflict region: %+v", c)
	}
	want := "# Refunds\n\nRefunds up to 1000 EUR.\n\n# Booking\n\nA party size.\nA date.\nA time.\n"
	if r.Merged != want || string([]rune(r.Merged)[c.Merged.Start:c.Merged.End]) != c.Target {
		t.Fatalf("merged with the target's side: %q %+v", r.Merged, c.Merged)
	}
	r = Merge3(mergeBase, src, tgt, nil, "source")
	if r.Merged != "# Refunds\n\nRefunds up to 500 EUR.\n\n# Booking\n\nA party size.\nA date.\nA time.\n" {
		t.Fatalf("prefer source: %q", r.Merged)
	}
	r = Merge3(mergeBase, src, tgt, map[int]string{0: "Refunds up to 750 EUR.\n"}, "")
	if !r.Clean || !r.Conflicts[0].Resolved || r.Merged != "# Refunds\n\nRefunds up to 750 EUR.\n\n# Booking\n\nA party size.\nA date.\nA time.\n" {
		t.Fatalf("resolved: %q", r.Merged)
	}
}

func TestMergeInsertsAtOnePlace(t *testing.T) {
	src := mergeBase + "A time.\n"
	tgt := mergeBase + "A phone.\n"
	r := Merge3(mergeBase, src, tgt, nil, "")
	if r.Clean || len(r.Conflicts) != 1 || r.Conflicts[0].Base != "" || r.Conflicts[0].Source != "A time.\n" {
		t.Fatalf("inserts: %+v", r)
	}
}

func TestMergeEmptyBase(t *testing.T) {
	r := Merge3("", "# New\n\nText.\n", "", nil, "")
	if !r.Clean || r.Merged != "# New\n\nText.\n" {
		t.Fatalf("empty base: %+v", r)
	}
}
