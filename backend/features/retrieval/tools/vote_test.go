package retrievaltools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Forge 15b: a vote with the same key twice moves the counts once.
func TestVoteOncePerKey(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, retrieval.Models()...); err != nil {
		t.Fatal(err)
	}
	args := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	segs := []retrieval.SegmentRef{{SourceID: "library", Key: "h1"}, {SourceID: "library", Key: "h2"}}
	counts := func() (up, down int) {
		var rows []retrieval.SegmentFeedback
		gdb.Find(&rows)
		for _, r := range rows {
			up, down = up+r.Up, down+r.Down
		}
		return
	}
	for i, want := range []bool{true, false} {
		cast, err := Vote(gdb, args, VoteArgs{Segments: segs, Verdict: "up", Key: "run-1"})
		if err != nil || cast != want {
			t.Fatalf("vote %d: cast %v, %v; want %v", i, cast, err, want)
		}
	}
	// A different verdict with the same key changes nothing either
	if cast, err := Vote(gdb, args, VoteArgs{Segments: segs, Verdict: "down", Key: "run-1"}); err != nil || cast {
		t.Fatalf("a second outcome counted: %v %v", cast, err)
	}
	if up, down := counts(); up != 2 || down != 0 {
		t.Fatalf("counts up %d down %d; want 2, 0", up, down)
	}
	if cast, _ := Vote(gdb, args, VoteArgs{Segments: segs[:1], Verdict: "down", Key: "run-2"}); !cast {
		t.Fatal("another key did not vote")
	}
	if up, down := counts(); up != 2 || down != 1 {
		t.Fatalf("counts up %d down %d; want 2, 1", up, down)
	}
	if _, err := Vote(gdb, args, VoteArgs{Segments: segs, Verdict: "meh", Key: "run-3"}); err == nil {
		t.Fatal("a verdict that is neither up nor down was taken")
	}
}
