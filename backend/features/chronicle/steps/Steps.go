// Package chroniclesteps is Chronicle's pipeline as step functions with
// typed inputs and outputs (Index.md › seam 3), each shaped like the
// Cognition node it becomes: Candidates (the Retrieve context and Active
// entries nodes), Match (stage 1, the Match context node) and Update (the
// memory update, the Update entry node). The window and the reply (stage 2)
// are conversationservice.Send's: the window reaches the steps as
// conversation.GatherArgs.Window, and the chosen entries reach the reply as
// pinned context items.
package chroniclesteps

import (
	"encoding/json"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"gorm.io/gorm"
)

// SourceID is the chronicle retrieval source's ID, and the SourceID of the
// segments the steps hand the reply.
const SourceID = "chronicle"

// Steps holds what the steps need.
type Steps struct {
	DB     *gorm.DB
	Chat   chatservice.Service
	Config config.Reader
	// Retriever returns the retrieval service, created after the registry
	// these steps are contributed to; nil leaves retrieval out.
	Retriever func() retrievalservice.Service
}

var log = logging.In("chronicle")

// level is the ai.chat.stages level of a stage, or the default level when
// the stage names none or one that does not exist.
func (s Steps) level(args features.ServiceArgs, stage func(aisettings.Stages) string) string {
	stages, err := config.Read[aisettings.Stages](s.Config, args, aisettings.StagesID)
	if err != nil {
		return ""
	}
	levels, err := config.Read[aisettings.Levels](s.Config, args, aisettings.LevelsID)
	if err != nil {
		return ""
	}
	if l, ok := levels.Find(stage(stages)); ok && stage(stages) != "" {
		return l.ID
	}
	return ""
}

// entries loads the principal's own entries of the given statuses (all
// when none are given) by ID: what the memory update may change.
func (s Steps) entries(args features.ServiceArgs, statuses ...string) (map[uint64]chronicle.Entry, error) {
	return s.load(chronicle.Owned(s.DB.WithContext(args.Context()), args.Principal), statuses)
}

// visible loads the entries the principal may use, shared ones included,
// of the given statuses by ID: what stage 1 may choose.
func (s Steps) visible(args features.ServiceArgs, statuses ...string) (map[uint64]chronicle.Entry, error) {
	return s.load(chronicle.Visible(s.DB.WithContext(args.Context()), args.Principal), statuses)
}

func (s Steps) load(q *gorm.DB, statuses []string) (map[uint64]chronicle.Entry, error) {
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var list []chronicle.Entry
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]chronicle.Entry, len(list))
	for _, e := range list {
		out[e.ID] = e
	}
	return out, nil
}

// decode reads a model's JSON reply into v, tolerating a markdown fence
// around it.
func decode(reply string, v any) error {
	reply = strings.TrimSpace(reply)
	if strings.HasPrefix(reply, "```") {
		reply = reply[strings.Index(reply, "\n")+1:] // after ```json
		reply = strings.TrimSuffix(strings.TrimSpace(reply), "```")
	}
	return json.Unmarshal([]byte(reply), v)
}

// clip cuts s to about n bytes on a rune boundary, marking the cut.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n] + "…"
}
