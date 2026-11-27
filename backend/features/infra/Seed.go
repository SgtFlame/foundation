package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/liveclient"
)

// ReadFolder reads an export's YAML files from a folder, by their paths
// relative to it (what infra.config.import takes as files).
func ReadFolder(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !(strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err == nil && len(files) == 0 {
		err = fmt.Errorf("no YAML files in %s", dir)
	}
	return files, err
}

// SeedResult is what SeedDraft did.
type SeedResult struct {
	Version   int  // the version the draft is
	Committed bool // false when the draft was that version already
}

// SeedDraft writes an export's files as a configuration's draft and
// commits it, marked applied when applied (forge 22o: the seed's first
// version describes what the seed made). Run again with the same files it
// commits nothing. The client signs in as the root tenant's administrator
// and switches to the root tenant.
func SeedDraft(ctx context.Context, c *liveclient.Client, config string, files map[string]string, message string, applied bool) (SeedResult, error) {
	if config == "" {
		config = DefaultConfig
	}
	if err := c.SignIn(ctx); err != nil {
		return SeedResult{}, err
	}
	if _, err := c.Post(ctx, "/identity/switch-tenant", map[string]string{"tenantId": strconv.FormatUint(features.RootTenantID, 10)}); err != nil {
		return SeedResult{}, fmt.Errorf("switching to the root tenant: %w", err)
	}
	read := func() (Config, error) {
		raw, err := c.Snapshot(ctx, "infra.config", json.RawMessage(`{"config":`+strconv.Quote(config)+`}`))
		if err != nil {
			return Config{}, err
		}
		var cfg Config
		_ = json.Unmarshal(raw, &cfg)
		return cfg, nil
	}
	send := func(cmd string, args any) (liveclient.Reply, error) {
		raw, _ := json.Marshal(args)
		r, err := c.Send(ctx, liveclient.Command{ID: c.NewID("seed"), Command: cmd, Target: config, Args: raw, Actor: "seed"})
		if err != nil {
			return r, err
		}
		return r, nil
	}
	doc, ps := Import(files)
	if len(ps) > 0 {
		return SeedResult{}, fmt.Errorf("the draft's files: %s: %s", ps[0].Path, ps[0].Message)
	}
	cfg, err := read()
	if err != nil {
		return SeedResult{}, err
	}
	// Already this document, committed: nothing to do (but marking applied).
	same := func(a, b Document) bool {
		x, _ := json.Marshal(a.Normalized())
		y, _ := json.Marshal(b.Normalized())
		return string(x) == string(y)
	}
	if cfg.Head > 0 && !cfg.Dirty() && same(cfg.Draft, doc) {
		return SeedResult{Version: cfg.Head}, nil
	}
	if r, err := send("infra.config.import", map[string]any{"files": files, "force": true}); err != nil || !r.OK {
		return SeedResult{}, replyErr("infra.config.import", r, err)
	}
	cfg, err = read()
	if err != nil {
		return SeedResult{}, err
	}
	r, err := send("infra.config.commit", map[string]any{"message": message, "expect": cfg.Rev, "applied": applied})
	if err != nil || !r.OK {
		if r.ErrorCode == "nothing_to_commit" {
			return SeedResult{Version: cfg.Head}, nil
		}
		return SeedResult{}, replyErr("infra.config.commit", r, err)
	}
	return SeedResult{Version: cfg.Head + 1, Committed: true}, nil
}

func replyErr(cmd string, r liveclient.Reply, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	msg := r.Message
	if len(r.Problems) > 0 {
		msg += " " + string(r.Problems)
	}
	return fmt.Errorf("%s was refused: %s", cmd, msg)
}
