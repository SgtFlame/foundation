package seedcheck_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra/seedcheck"
)

const dir = "../../../scripts/infra/seed"

func TestSeedIsIdempotent(t *testing.T) {
	for _, mode := range []string{"adopt", "lxd"} {
		t.Run(mode, func(t *testing.T) {
			r, err := seedcheck.Check(context.Background(), dir, mode)
			if err != nil {
				t.Fatal(err)
			}
			if !r.OK() {
				t.Fatalf("%v\nfirst run: %v\n%s\nsecond run:\n%s", r.Problems(), r.Runs[0].Changes, r.Runs[0].Output, r.Runs[1].Output)
			}
			first := strings.Join(r.Runs[0].Changes, "\n")
			for _, want := range []string{"snap install lxd", "lxc launch ubuntu:24.04 nats-lab-1", "lxc launch ubuntu:24.04 server-lab-1",
				"server-lab-1 apt-get install axiom-server axiomd", "nats-lab-1 systemctl enable --now nats-server",
				"server-lab-1 setup tony", "server-lab-1 import /var/lib/axiom-seed/draft"} {
				if !strings.Contains(first, want) {
					t.Errorf("the first run did not %q:\n%s", want, first)
				}
			}
			if mode == "lxd" && !strings.Contains(first, "lxc launch ubuntu:24.04 axiom-seed --vm -c security.nesting=true") {
				t.Errorf("no VM:\n%s", first)
			}
			if !strings.Contains(r.Runs[1].Output, "sign in as tony at http://10.1.2.3:8080") {
				t.Errorf("second run's output:\n%s", r.Runs[1].Output)
			}
		})
	}
}

// The seed refuses what it cannot do before touching anything.
func TestSeedRefuses(t *testing.T) {
	r, err := seedcheck.Check(context.Background(), dir, "adopt", "--site", "Not A Site")
	if err != nil {
		t.Fatal(err)
	}
	if r.Runs[0].Err == "" || len(r.Runs[0].Changes) != 0 || !strings.Contains(r.Runs[0].Output, "--site is lower case") {
		t.Fatalf("%+v", r.Runs[0])
	}
}
