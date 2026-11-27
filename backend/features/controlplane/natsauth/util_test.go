package natsauth

import (
	"os"
	"path/filepath"
)

func osRemove(dir, name string) error { return os.Remove(filepath.Join(dir, name)) }
