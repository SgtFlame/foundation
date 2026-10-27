package db

import (
	"path/filepath"
	"testing"
)

type note struct {
	Model
	Title string
}

func TestMigrateAssignsTSID(t *testing.T) {
	gdb, err := Open(WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, &note{}); err != nil {
		t.Fatal(err)
	}

	a, b := note{Title: "a"}, note{Title: "b"}
	if err := gdb.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || b.ID <= a.ID {
		t.Fatalf("IDs %d, %d are not increasing TSIDs", a.ID, b.ID)
	}

	var got note
	if err := gdb.First(&got, b.ID).Error; err != nil || got.Title != "b" {
		t.Fatalf("read back %+v, %v", got, err)
	}

	keep := note{Model: Model{ID: 42}, Title: "fixed"}
	if err := gdb.Create(&keep).Error; err != nil || keep.ID != 42 {
		t.Fatalf("preset ID became %d, %v", keep.ID, err)
	}
}
