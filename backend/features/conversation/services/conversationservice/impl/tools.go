package impl

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
)

const toolsPrompt = "\n\n# Tools\nYou can call tools to search and read the user's Volumes and Chronicles, to ask them to choose, to open things in their workbench, and to change their files and Chronicles. Every change is shown to the user to approve first, so propose it with the tool rather than asking in words. When a tool's result has passages with a cite such as [5], cite them like the context's passages. Never say something was changed unless a tool's result says so."

// maxClientTools bounds the client tools a message may declare.
const maxClientTools = 20

// waiting is a tool call its round is paused on, until Answer.
type waiting struct {
	owner   features.Principal
	answers chan conversationservice.AnswerArgs
}

func waitKey(roundID uint64, callID string) string {
	return strconv.FormatUint(roundID, 10) + "/" + callID
}

// expect makes the place a call's answer arrives, before the browser hears
// of the call.
func (s *service) expect(owner features.Principal, roundID uint64, callID string) *waiting {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := &waiting{owner: owner, answers: make(chan conversationservice.AnswerArgs, 1)}
	s.waiting[waitKey(roundID, callID)] = w
	return w
}

func (s *service) waitingFor(roundID uint64, callID string) *waiting {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting[waitKey(roundID, callID)]
}

func (s *service) forget(roundID uint64, callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiting, waitKey(roundID, callID))
}

// gate pauses a round's calls for its user: Approve for writes, RunClient
// for the browser's tools. Both end when the round is stopped.
type gate struct {
	s    *service
	args conversationservice.SendArgs
	r    *round
}

func (g gate) wait(call tools.ToolCall, d time.Duration) (conversationservice.AnswerArgs, error) {
	defer g.s.forget(call.RoundID, call.CallID)
	w := g.s.waitingFor(call.RoundID, call.CallID)
	if w == nil {
		return conversationservice.AnswerArgs{}, errors.New("the call was not expected")
	}
	var timeout <-chan time.Time
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case a := <-w.answers:
		return a, nil
	case <-timeout:
		return conversationservice.AnswerArgs{}, tools.ErrTimeout
	case <-g.args.Context().Done():
		return conversationservice.AnswerArgs{}, context.Cause(g.args.Context())
	}
}

func (g gate) Approve(call tools.ToolCall) (tools.Approval, error) {
	a, err := g.wait(call, tools.WaitForUser)
	if err != nil {
		return tools.ApprovalDenied, err
	}
	if a.Approval == tools.ApprovalAlways {
		g.s.allow(g.args, g.r, call.Name)
	}
	return a.Approval, nil
}

func (g gate) RunClient(call tools.ToolCall, d time.Duration) (json.RawMessage, error) {
	a, err := g.wait(call, d)
	switch {
	case err != nil:
		return nil, err
	case a.Error != "":
		return nil, errors.New(clip(a.Error, 2000))
	}
	return a.Result, nil
}

// allow stores "Always allow" for a tool in the conversation.
func (s *service) allow(args conversationservice.SendArgs, r *round, name string) {
	var allowed []string
	_ = json.Unmarshal([]byte(r.conversation.AllowedTools), &allowed)
	if slices.Contains(allowed, name) {
		return
	}
	allowed = append(allowed, name)
	stored, _ := json.Marshal(allowed)
	r.conversation.AllowedTools = string(stored)
	persist := context.WithoutCancel(args.Context())
	if err := s.db.WithContext(persist).Model(&r.conversation).Update("allowed_tools", r.conversation.AllowedTools).Error; err != nil {
		log.With("conversationId", r.conversation.ID).Error(persist, err, "cannot store the tools allowed in conversation %d", r.conversation.ID)
	}
}

// allowed is the conversation's "Always allow" tools.
func allowed(r *round) map[string]bool {
	var names []string
	_ = json.Unmarshal([]byte(r.conversation.AllowedTools), &names)
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// offered is the tools the round may call: none when the conversation's
// tools setting is off, else the server's and the browser's, less those
// the tools section turns off.
func (s *service) offered(args conversationservice.SendArgs, r *round) ([]tools.Tool, toolssettings.Tools) {
	settings, err := config.Read[toolssettings.Tools](s.config, args.ServiceArgs, toolssettings.ID)
	if err != nil {
		settings = toolssettings.Defaults
	}
	if !enabled(s.settingsOf(args, r, tools.ContextID, nil)) {
		return nil, settings
	}
	return tools.Enabled(tools.Tools.All(s.registry), args.ClientTools, settings), settings
}

// checkClientTools rejects client tools the browser may not declare.
func checkClientTools(args conversationservice.SendArgs) *features.ServiceResponse {
	if len(args.ClientTools) > maxClientTools {
		return args.Fail(400, log.Code("too_many_client_tools").Errorf("a message declares at most %d client tools", maxClientTools))
	}
	for _, t := range args.ClientTools {
		switch {
		case t.ID == "" || len(t.ID) > 64:
			return args.Fail(400, log.Code("invalid_client_tool").New("a client tool needs a name of at most 64 characters"))
		case t.Effect != tools.EffectRead && t.Effect != tools.EffectUI:
			return args.Fail(400, log.Code("invalid_client_tool").With("tool", t.ID).Errorf("client tool %s must be read or ui, not %q", t.ID, t.Effect))
		case len(t.Input) > 0 && !json.Valid(t.Input):
			return args.Fail(400, log.Code("invalid_client_tool").With("tool", t.ID).Errorf("client tool %s has an invalid input schema", t.ID))
		}
	}
	return nil
}

// citer numbers a round's tool results as its next citations, after its
// context: a segment already cited keeps its number. New ones are stored
// and the round's sources sent again.
type citer struct {
	mu   sync.Mutex
	s    *service
	args conversationservice.SendArgs
	r    *round
	emit func(conversationservice.SendEvent)
}

func (c *citer) cite(segments []retrieval.Scored) []retrieval.Scored {
	c.mu.Lock()
	defer c.mu.Unlock()
	var added []retrieval.RoundSegment
	for i, seg := range segments {
		at := slices.IndexFunc(c.r.segments, func(rs retrieval.RoundSegment) bool { return rs.SourceID == seg.SourceID && rs.SegmentKey == seg.Key })
		if at >= 0 {
			segments[i].Number = c.r.segments[at].Number
			continue
		}
		segments[i].Number = len(c.r.segments) + len(added) + 1
		added = append(added, retrieval.RoundSegmentFrom(c.r.round.ID, c.r.round.Tenanted, segments[i]))
	}
	if len(added) == 0 {
		return segments
	}
	persist := context.WithoutCancel(c.args.Context())
	if err := c.s.db.WithContext(persist).Create(&added).Error; err != nil {
		log.With("roundId", c.r.round.ID).Error(persist, err, "cannot store the tool citations of round %d", c.r.round.ID)
	}
	c.r.segments = append(c.r.segments, added...)
	c.emit(conversationservice.SendEvent{Kind: conversationservice.EventSources, Segments: slices.Clone(c.r.segments)})
	return segments
}
