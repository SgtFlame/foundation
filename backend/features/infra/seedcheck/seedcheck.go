// Package seedcheck runs the seed (backend/scripts/infra/seed/seed.sh,
// forge 22o) in a stub environment: fake lxc, lxd and snap on the PATH
// that keep their world in files and record every call. It runs the seed
// twice and reports what each run changed, so a test and Forge's
// seed-check tool can tell that the second run changes nothing and that
// the password never reached a command line.
package seedcheck

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed fakes
var fakes embed.FS

// Password is the administrator's password the check feeds the seed.
const Password = "seed-check-password-1"

// Run is one run of the seed.
type Run struct {
	Changes []string `json:"changes"` // the calls that changed something, in order
	Output  string   `json:"output"`
	Err     string   `json:"error,omitempty"`
}

// Report is what Check found.
type Report struct {
	Mode   string `json:"mode"`
	Runs   []Run  `json:"runs"`
	Leaked bool   `json:"leaked"` // the password was on a command line or in the output
}

// OK tells whether both runs succeeded, the first changed something, the
// second nothing, and the password stayed off command lines.
func (r Report) OK() bool {
	return len(r.Runs) == 2 && r.Runs[0].Err == "" && r.Runs[1].Err == "" && len(r.Runs[0].Changes) > 0 && len(r.Runs[1].Changes) == 0 && !r.Leaked
}

// Problems are the reasons OK is false.
func (r Report) Problems() []string {
	var out []string
	for i, run := range r.Runs {
		if run.Err != "" {
			out = append(out, fmt.Sprintf("run %d failed: %s", i+1, run.Err))
		}
	}
	if len(r.Runs) == 2 {
		if len(r.Runs[0].Changes) == 0 {
			out = append(out, "the first run changed nothing")
		}
		if len(r.Runs[1].Changes) > 0 {
			out = append(out, "the second run changed: "+strings.Join(r.Runs[1].Changes, "; "))
		}
	}
	if r.Leaked {
		out = append(out, "the password was on a command line or in the output")
	}
	return out
}

// Check runs dir/seed.sh twice in mode ("adopt", or "lxd" for --provider
// lxd) with the given extra arguments in a fresh stub world.
func Check(ctx context.Context, dir, mode string, extra ...string) (Report, error) {
	script := filepath.Join(dir, "seed.sh")
	if _, err := os.Stat(script); err != nil {
		return Report{}, err
	}
	tmp, err := os.MkdirTemp("", "seedcheck")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return Report{}, err
	}
	entries, _ := fakes.ReadDir("fakes")
	for _, e := range entries {
		raw, _ := fakes.ReadFile("fakes/" + e.Name())
		if err := os.WriteFile(filepath.Join(bin, e.Name()), raw, 0o755); err != nil {
			return Report{}, err
		}
	}
	key := filepath.Join(tmp, "repo.asc")
	_ = os.WriteFile(key, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nfake\n-----END PGP PUBLIC KEY BLOCK-----\n"), 0o644)
	args := []string{"--site", "lab", "--site-name", "Lab", "--repo", "https://repo.invalid/axiom", "--repo-key", key, "--admin", "tony"}
	switch mode {
	case "", "adopt":
		mode = "adopt"
		args = append([]string{"--adopt"}, args...)
	case "lxd":
		args = append([]string{"--provider", "lxd", "--name", "axiom-seed"}, args...)
	default:
		return Report{}, fmt.Errorf("mode is adopt or lxd")
	}
	args = append(args, extra...)
	argsLog := filepath.Join(tmp, "args")
	rep := Report{Mode: mode}
	for i := 1; i <= 2; i++ {
		changes := filepath.Join(tmp, fmt.Sprintf("changes%d", i))
		_ = os.WriteFile(changes, nil, 0o644)
		cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FAKE_STATE="+filepath.Join(tmp, "state"),
			"FAKE_LOG="+changes, "FAKE_ARGS="+argsLog, "SEED_NOT_ROOT=1", "SEED_WAIT_SECONDS=0")
		cmd.Stdin = strings.NewReader(Password + "\n")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		run := Run{}
		if err := cmd.Run(); err != nil {
			run.Err = err.Error()
		}
		run.Output = out.String()
		raw, _ := os.ReadFile(changes)
		for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if l != "" {
				run.Changes = append(run.Changes, strings.TrimPrefix(l, "+ "))
			}
		}
		if strings.Contains(run.Output, Password) {
			rep.Leaked = true
		}
		rep.Runs = append(rep.Runs, run)
	}
	if raw, _ := os.ReadFile(argsLog); bytes.Contains(raw, []byte(Password)) {
		rep.Leaked = true
	}
	return rep, nil
}
