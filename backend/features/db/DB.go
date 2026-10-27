package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Option configures the database opened by Open.
type Option func(*config)

type config struct {
	path string
}

// WithPath sets the SQLite database file; ":memory:" opens a private in-memory
// database.
func WithPath(path string) Option {
	return func(c *config) {
		c.path = path
	}
}

// Open opens the relational database, SQLite by default at
// ~/.foundation/foundation.db, creating its directory if needed.
func Open(opts ...Option) (*gorm.DB, error) {
	c := config{path: DefaultPath()}
	for _, opt := range opts {
		opt(&c)
	}

	dsn := "file::memory:?_pragma=foreign_keys(1)"
	if c.path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
			return nil, err
		}
		dsn = c.path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}

	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", c.path, err)
	}
	return gdb, nil
}

// DefaultPath is ~/.foundation/foundation.db, or the same under the temp
// directory when there is no home directory.
func DefaultPath() string {
	base, err := os.UserHomeDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, ".foundation", "foundation.db")
}
