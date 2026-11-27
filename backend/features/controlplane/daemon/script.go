package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
)

// ScriptArgs are a script request's (forge 20c-3): the script's hash, its
// arguments as a JSON object (written to a file passed as $1, part
// question 1), its environment (only these and PATH) and its timeout.
type ScriptArgs struct {
	Hash           string            `json:"hash"`
	Args           json.RawMessage   `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeoutSeconds,omitempty"`
}

// MaxScriptOutput is how much of a run's output goes to CTL; the rest is
// counted.
const MaxScriptOutput = 64 << 10

// ScriptPath is PATH in a script's environment.
var ScriptPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// scriptKeys are the trusted signing keys: desired state's scriptKeys,
// else the config's.
func (d *Daemon) scriptKeys(ctx context.Context) []string {
	raw, ok, err := d.c.Desired(ctx, d.target)
	if err == nil && ok {
		var doc struct {
			ScriptKeys []string `json:"scriptKeys"`
		}
		if json.Unmarshal(raw, &doc) == nil && len(doc.ScriptKeys) > 0 {
			return doc.ScriptKeys
		}
	}
	return d.Config.ScriptKeys
}

// script fetches a signed script, checks it and runs it under bash with
// a clean environment, its output lines reported as progress.
func (d *Daemon) script(ctx context.Context, r ctl.Request, progress func(msg string, seq int)) error {
	var a ScriptArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Hash == "" {
		return log.Code("bad_args").Errorf("a script request names a hash")
	}
	content, err := d.c.FetchScript(ctx, a.Hash, d.scriptKeys(ctx))
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "axiomd-script-")
	if err != nil {
		return log.Wrap(err)
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(script, content, 0o700); err != nil {
		return log.Wrap(err)
	}
	argsFile := filepath.Join(dir, "args.json")
	args := a.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := os.WriteFile(argsFile, args, 0o600); err != nil {
		return log.Wrap(err)
	}
	timeout := time.Duration(a.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(rctx, "bash", script, argsFile)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + ScriptPath}
	keys := make([]string, 0, len(a.Env))
	for k := range a.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k != "PATH" {
			cmd.Env = append(cmd.Env, k+"="+a.Env[k])
		}
	}
	cmd.WaitDelay = 2 * time.Second
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return log.Wrap(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sent, dropped, seq := 0, 0, 0
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if sent+len(line) > MaxScriptOutput {
				dropped += len(line) + 1
				continue
			}
			sent += len(line) + 1
			seq++
			progress(line, seq)
		}
		_, _ = io.Copy(io.Discard, pr)
		if dropped > 0 {
			progress(fmt.Sprintf("… %d more bytes of output not kept", dropped), seq+1)
		}
	}()
	err = cmd.Wait()
	_ = pw.Close()
	<-done
	if rctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return log.Code("script_timeout").Errorf("the script ran past %s and was killed", timeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return log.Code("script_failed").Errorf("the script exited with %d", exit.ExitCode())
	}
	return log.Wrap(err)
}
