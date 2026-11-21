package jobs

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the job resource type: anyone who may run
// graphs (execute) enqueues jobs, which run as themselves, and sees,
// cancels and retries their own; job.manage sees and acts on every job of
// the tenant.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "job", Source: "jobs", Order: 94}, Title: "Background jobs", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See your own background jobs", Class: authz.ClassRead},
			{Name: "create", Title: "Enqueue background jobs that run as you, and cancel or retry them", Class: authz.ClassExecute},
			{Name: "manage", Title: "See, cancel and retry everyone's background jobs", Class: authz.ClassAdmin},
		},
	})
}

// SettingsID is the jobs section.
const SettingsID = "jobs"

// Settings are the jobs section: how jobs run on each server, per tenant.
type Settings struct {
	// MaxConcurrent is how many jobs of one queue a server runs at once.
	MaxConcurrent int `json:"maxConcurrent"`
	// Queues override MaxConcurrent per queue.
	Queues map[string]QueueSettings `json:"queues,omitempty"`
	// MaxAttempts is how many times a job runs before it is dead.
	MaxAttempts int `json:"maxAttempts"`
	// BackoffSeconds is the first retry's wait, doubled each attempt up to
	// MaxBackoffSeconds.
	BackoffSeconds    int `json:"backoffSeconds"`
	MaxBackoffSeconds int `json:"maxBackoffSeconds"`
	// DoneHours is how long jobs.list shows done and canceled jobs.
	DoneHours int `json:"doneHours"`
}

// QueueSettings are one queue's.
type QueueSettings struct {
	MaxConcurrent int `json:"maxConcurrent"`
}

// Defaults are the section's defaults, used when it cannot be read.
var Defaults = Settings{MaxConcurrent: 4, MaxAttempts: 8, BackoffSeconds: 2, MaxBackoffSeconds: 600, DoneHours: 24}

const settingsSchema = `{
  "type": "object",
  "properties": {
    "maxConcurrent": {"type": "integer", "title": "Jobs at once per queue", "description": "How many jobs of one queue each server runs at the same time.", "min": 1, "max": 256},
    "queues": {"type": "object", "title": "Queues", "description": "Per queue: {maxConcurrent}, in place of the above."},
    "maxAttempts": {"type": "integer", "title": "Attempts", "description": "How many times a failing job runs before it is dead, when it fails in a way worth retrying.", "min": 1, "max": 100},
    "backoffSeconds": {"type": "integer", "title": "First retry after (s)", "description": "The wait before the first retry; each later one waits twice as long.", "min": 1, "max": 3600},
    "maxBackoffSeconds": {"type": "integer", "title": "Longest wait (s)", "description": "The longest wait between retries.", "min": 1, "max": 86400},
    "doneHours": {"type": "integer", "title": "Show finished jobs for (h)", "description": "How long the Jobs view lists jobs that are done or canceled.", "min": 1, "max": 720}
  }
}`

// ContributeSettings registers the jobs section.
func ContributeSettings(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: SettingsID, Order: 910, Source: "jobs"},
		Title:       "Background jobs",
		Description: "How background jobs run: how many at once on each server, how often a failing job is retried and how long between tries.",
		Category:    []string{"System", "Background jobs"},
		Scopes:      []string{"app", "tenant"},
		Schema:      json.RawMessage(settingsSchema),
		Defaults:    defaults,
		Version:     1,
	})
}

// settingsOf fills what the section leaves unset.
func settingsOf(s Settings) Settings {
	d := Defaults
	for _, f := range []struct{ v, def *int }{{&s.MaxConcurrent, &d.MaxConcurrent}, {&s.MaxAttempts, &d.MaxAttempts},
		{&s.BackoffSeconds, &d.BackoffSeconds}, {&s.MaxBackoffSeconds, &d.MaxBackoffSeconds}, {&s.DoneHours, &d.DoneHours}} {
		if *f.v <= 0 {
			*f.v = *f.def
		}
	}
	return s
}

// concurrency is how many jobs of queue a server runs at once.
func (s Settings) concurrency(queue string) int {
	if q, ok := s.Queues[queue]; ok && q.MaxConcurrent > 0 {
		return q.MaxConcurrent
	}
	return s.MaxConcurrent
}
