package impl

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// The pre-mode's outcomes, on conversation.PreModeDecision.Outcome.
const (
	preStayed        = "stayed"
	preSwitched      = "switched"
	preAskedSwitched = "asked-switched"
	preAskedStayed   = "asked-stayed"
	preTimeout       = "timeout"
	preFailed        = "failed"
)

// askWait is how long a pre-mode question waits for its answer before the
// round goes on in its mode.
const askWait = 10 * time.Minute

// preModeActor marks the pre-mode's question among a round's tool calls
// (its NodeID), so a reload shows it where it was asked.
const preModeActor = "premode"

// preMode runs the chat section's pre-mode graph before the round's mode
// graph (forge 15f): it keeps the round's mode, switches it (stored, with
// a mode event the browser shows with Undo), or asks the person on a
// chat.askChoice card. Whatever goes wrong, the round stays in its mode.
// The decision is stored on the round. The graph decides; only the
// threshold, the first switch and the budget are applied here, from the
// settings.
func (s *service) preMode(args conversationservice.SendArgs, r *round, h *host) {
	set := r.chat.PreMode
	if !set.On() || args.ModeChosen || args.Undoes != 0 {
		return
	}
	modes, err := conversationsettings.ReadModes(s.config, args.ServiceArgs)
	if err != nil {
		return
	}
	var offered []conversationsettings.Mode
	for _, m := range modes.Modes {
		if slices.Contains(args.Offered, m.ID) {
			offered = append(offered, m)
		}
	}
	if len(offered) < 2 {
		return
	}
	def, _ := modes.Default()
	first := !s.everSwitched(args, r)
	listed := make([]map[string]any, len(offered))
	for i, m := range offered {
		listed[i] = map[string]any{"id": m.ID, "icon": m.Icon, "label": m.Label, "purpose": s.purposeOf(args, m)}
	}
	var previous any
	if n := len(r.history); n > 0 {
		last := r.history[n-1]
		previous = map[string]any{"mode": last.Mode, "reply": last.Reply}
	}

	budget := set.Budget()
	ctx, cancel := context.WithTimeout(args.Context(), budget)
	defer cancel()
	call := args.ServiceArgs
	call.Ctx = ctx
	call.Meta = features.Meta{Feature: "chat-premode", ConversationID: r.conversation.ID, RoundID: r.round.ID, Debug: r.debug}
	start := time.Now()
	res, err := s.engine.Run(engine.RunArgs{
		ServiceArgs: call, GraphID: set.GraphID(), Event: cognition.EventCall, Trigger: "chat-premode",
		Limits: &engine.Limits{MaxWallMs: int(budget.Milliseconds())},
		Inputs: cognition.Values{
			conversationnodes.PreText: r.round.Text, conversationnodes.PreMode: r.mode.ID, conversationnodes.PreDefaultMode: def.ID,
			conversationnodes.PreOffered: listed, conversationnodes.PreWorkbench: json.RawMessage(orEmpty(args.Workbench)),
			conversationnodes.PreTitles: s.recentTitles(args, r), conversationnodes.PreFirstSwitch: first, conversationnodes.PrePrevious: previous,
		},
	})
	d := conversation.PreModeDecision{From: r.mode.ID, Ms: time.Since(start).Milliseconds()}
	switch {
	case err != nil && (errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded || time.Since(start) >= budget):
		d.Outcome, d.Error = preTimeout, clip(err.Error(), 500)
	case err != nil:
		d.Outcome, d.Error = preFailed, clip(err.Error(), 500)
	default:
		d.Decision, _ = res.Outputs["decision"].(string)
		d.Mode, _ = res.Outputs["mode"].(string)
		d.Reason, _ = res.Outputs["reason"].(string)
		d.Confidence = number(res.Outputs["confidence"])
	}
	if err != nil {
		log.With("roundId", r.round.ID, "graph", set.GraphID()).Info(args.Context(), "the pre-mode did not decide, so the round stays in %s: %v", r.mode.ID, err)
	}

	at := slices.IndexFunc(offered, func(m conversationsettings.Mode) bool { return m.ID == d.Mode })
	if d.Outcome == "" {
		d.Outcome = preStayed
	}
	if err == nil && at >= 0 && offered[at].ID != r.mode.ID && (d.Decision == "switch" || d.Decision == "ask") {
		target := offered[at]
		if d.Decision == "switch" && d.Confidence >= set.Bar() && !(first && set.Asks1st()) {
			d.Outcome = preSwitched
			s.switchTo(args, r, h, target, d.Reason)
		} else if s.askSwitch(args, r, h, target, d.Reason) {
			d.Outcome = preAskedSwitched
			s.switchTo(args, r, h, target, d.Reason)
		} else {
			d.Outcome = preAskedStayed
		}
	}
	raw, _ := json.Marshal(d)
	r.round.PreMode = string(raw)
	persist := context.WithoutCancel(args.Context())
	if err := s.db.WithContext(persist).Model(&r.round).Update("pre_mode", r.round.PreMode).Error; err != nil {
		log.With("roundId", r.round.ID).Error(persist, err, "cannot store the pre-mode decision of round %d", r.round.ID)
	}
}

// number reads a graph's number output, which may come as any numeric type.
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// everSwitched reports whether the conversation has switched modes
// before: a round ran in another mode than its current one (/mode, the
// menu), or the pre-mode switched one. Its first switch asks (question 6).
func (s *service) everSwitched(args conversationservice.SendArgs, r *round) bool {
	if r.conversation.ID == 0 {
		return false
	}
	var n int64
	err := s.owned(args.ServiceArgs).Model(&conversation.Round{}).
		Where("conversation_id = ? AND id <> ? AND ((mode <> '' AND mode <> ?) OR pre_mode LIKE ?)", r.conversation.ID, r.round.ID, r.mode.ID, `%switched"%`).Count(&n).Error
	return err == nil && n > 0
}

// recentTitles are the titles of the person's latest other conversations.
func (s *service) recentTitles(args conversationservice.SendArgs, r *round) []string {
	var titles []string
	_ = s.owned(args.ServiceArgs).Model(&conversation.Conversation{}).
		Where("id <> ? AND title <> ''", r.conversation.ID).Order("last_round_at DESC").Limit(5).Pluck("title", &titles).Error
	if titles == nil {
		titles = []string{}
	}
	return titles
}

// purposeOf is what a mode is for: its own purpose, else its contribution's
// (a mode list stored before purposes existed), else the start of its
// graph's description.
func (s *service) purposeOf(args conversationservice.SendArgs, m conversationsettings.Mode) string {
	if m.Purpose != "" {
		return m.Purpose
	}
	for _, e := range conversation.Modes.All(s.registry) {
		if e.ID == m.ID && e.Purpose != "" {
			return e.Purpose
		}
	}
	if g, ok := s.engine.Graph(args.ServiceArgs, m.GraphRef); ok {
		return clip(g.Description, 300)
	}
	return ""
}

// switchTo moves the round and its conversation to another mode, stores
// it, and tells the browser.
func (s *service) switchTo(args conversationservice.SendArgs, r *round, h *host, to conversationsettings.Mode, reason string) {
	from := r.mode.ID
	r.mode = to
	r.round.Mode, r.conversation.Mode = to.ID, to.ID
	persist := context.WithoutCancel(args.Context())
	if err := s.db.WithContext(persist).Model(&r.conversation).Update("mode", to.ID).Error; err != nil {
		log.With("conversationId", r.conversation.ID).Error(persist, err, "cannot store the mode of conversation %d", r.conversation.ID)
	}
	if err := s.db.WithContext(persist).Model(&r.round).Update("mode", to.ID).Error; err != nil {
		log.With("roundId", r.round.ID).Error(persist, err, "cannot store the mode of round %d", r.round.ID)
	}
	switched := r.round
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventMode, Round: &switched, Switch: &conversationservice.Switch{From: from, To: to.ID, Reason: reason}})
}

// askSwitch asks the person whether to switch, on a chat.askChoice card the
// server issues (stored as the round's tool call, so a reload shows it),
// and waits for the answer: true to switch. No answer, another answer or
// a stopped round stays.
func (s *service) askSwitch(args conversationservice.SendArgs, r *round, h *host, to conversationsettings.Mode, reason string) bool {
	title := func(m conversationsettings.Mode) string {
		if m.Icon != "" {
			return m.Icon + " " + m.Label
		}
		return m.Label
	}
	question := reason
	if question == "" {
		question = "This looks like a job for " + title(to) + "."
	}
	question += " Switch to " + title(to) + "?"
	in, _ := json.Marshal(map[string]any{"question": question, "choices": []map[string]string{
		{"label": "Switch to " + title(to), "value": "switch"},
		{"label": "Stay in " + title(r.mode), "value": "stay"},
	}})
	tc := tools.ToolCall{
		Tenanted: r.round.Tenanted, Owned: r.round.Owned, ConversationID: r.conversation.ID, RoundID: r.round.ID, NodeID: preModeActor,
		CallID: "premode-" + strconv.FormatUint(r.round.ID, 10), Name: "chat.askChoice", Title: "Which mode?",
		Effect: tools.EffectUI, Location: tools.LocationClient, Args: string(in), Status: tools.StatusClient,
	}
	persist := context.WithoutCancel(args.Context())
	if err := s.db.WithContext(persist).Create(&tc).Error; err != nil {
		log.With("roundId", r.round.ID).Error(persist, err, "cannot store the pre-mode question of round %d", r.round.ID)
		return false
	}
	s.expect(args.Principal, tc.RoundID, tc.CallID)
	asked := tc
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventTool, ToolCall: &asked})
	start := time.Now()
	a, err := gate{s: s, args: args, r: r}.wait(tc, askWait)
	var choice struct {
		Value string `json:"value"`
	}
	if err == nil {
		_ = json.Unmarshal(a.Result, &choice)
	}
	switching := err == nil && choice.Value == "switch"
	tc.DurationMs = time.Since(start).Milliseconds()
	switch {
	case errors.Is(err, tools.ErrTimeout):
		tc.Status, tc.Summary = tools.StatusTimeout, "No answer: stayed in "+title(r.mode)
	case err != nil:
		tc.Status, tc.Error = tools.StatusDenied, "Stopped"
	case switching:
		tc.Status, tc.Result, tc.Summary = tools.StatusDone, string(a.Result), "Switched to "+title(to)
	default:
		tc.Status, tc.Result, tc.Summary = tools.StatusDone, string(a.Result), "Stayed in "+title(r.mode)
	}
	if err := s.db.WithContext(persist).Save(&tc).Error; err != nil {
		log.With("roundId", r.round.ID).Error(persist, err, "cannot store the answer to the pre-mode question of round %d", r.round.ID)
	}
	answered := tc
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventTool, ToolCall: &answered})
	return switching
}
