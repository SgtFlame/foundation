package installs_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/branches"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Publishing a commit (forge 19c, iteration question 6): once an app has
// commits only main's head is published, and only while main has no
// uncommitted changes; the version names the commit.
func TestPublishCommit(t *testing.T) {
	f := setup(t)
	seq := f.tasks(true)
	f.draft("tasks", seq)
	read := func() string {
		rec, _ := branches.GetRecord(context.Background(), f.p, 1, "tasks", live.Main)
		return rec.Head
	}
	last := ""
	head := func() string {
		eventually(t, "the head moving", func() bool { h := read(); return h != "" && h != last })
		last = read()
		return last
	}

	f.ok("app.commit", "", map[string]any{"app": "tasks", "message": "The first version"})
	first := head()
	f.refused("app.publish", "tasks", map[string]any{"rev": 1}, "args.commit", "only a commit on main is published")

	// A change after the head stops the publish until it is committed
	f.ok("app.setInfo", "tasks", map[string]any{"description": "Who does what"})
	f.refused("app.publish", "tasks", map[string]any{"commit": first}, "args.commit", "main has changes after")
	f.ok("app.commit", "", map[string]any{"app": "tasks", "message": "Describes the app"})
	second := head()
	f.refused("app.publish", "tasks", map[string]any{"commit": first}, "args.commit", "not main's head")

	res := f.ok("app.publish", "tasks", map[string]any{"commit": second})
	raw := f.wait(manifests.VersionProjector, manifests.VersionTargetType+"."+apps.VersionID("tasks", 1), res.Seq).Value
	var v apps.Version
	if err := json.Unmarshal(raw, &v); err != nil || v.Commit != second || v.Manifest.Description != "Who does what" {
		t.Errorf("version: commit %q, description %q, %v", v.Commit, v.Manifest.Description, err)
	}
}
