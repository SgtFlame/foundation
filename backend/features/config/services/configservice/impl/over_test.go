package impl

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	aisettings "github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/events"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
)

// A Set in one process reaches config.Read's cache in another through
// config.Changes over NATS (forge 21f-2).
func TestChangesReachAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	ns := natsd.StartTest(t)
	process := func(name string) *service {
		gdb, err := db.Open(db.WithPath(path))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(gdb, config.Models()...); err != nil {
			t.Fatal(err)
		}
		reg := extensions.NewRegistry()
		aisettings.Contribute(reg)
		cipher, _ := config.NewCipher(make([]byte, 32))
		changes := config.NewChanges()
		nc, err := ns.Connect(name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		stop, err := events.Over(changes, nc, "config.changes")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(stop)
		return newService(WithDB(gdb), WithRegistry(reg), WithCipher(cipher), WithChanges(changes))
	}
	a, b := process("a"), process("b")
	before, err := b.Resolved(local, aisettings.LevelsID) // cached in b
	if err != nil {
		t.Fatal(err)
	}
	if resp := set(t, a, aisettings.LevelsID, `{"levels": [{"id": "only", "label": "Only", "modelRef": "gemini-2.5-pro", "default": true}]}`, 0); resp.Revision != 1 {
		t.Fatalf("set: %+v", resp.Common)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		after, err := b.Resolved(local, aisettings.LevelsID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(after), `"only"`) {
			break
		}
		if string(after) != string(before) || time.Now().After(deadline) {
			t.Fatalf("b still reads %s", after)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
