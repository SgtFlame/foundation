package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"gorm.io/gorm"
)

const (
	maxText      = 32 << 10 // a message
	maxWorkbench = 16 << 10 // a workbench snapshot; the browser keeps it near 4 KB
	// roundWall is how long a round's mode graph may run: a round runs
	// until its user stops it, and its graph tools may run agents.
	roundWall = 2 * time.Hour
)

// round is what Send has resolved and stored before the mode graph runs.
type round struct {
	conversation conversation.Conversation
	round        conversation.Round
	chat         conversationsettings.Chat
	mode         conversationsettings.Mode
	levels       aisettings.Levels
	level        aisettings.Level
	model        aisettings.Model
	models       aisettings.Models                // the catalog, for a reply at another level
	history      []conversation.Round             // the window: the rounds sent as history, oldest first
	settings     map[string]json.RawMessage       // the context settings, by provider ID
	segments     []retrieval.RoundSegment         // what the round injected, once stored
	activity     []conversation.Gesture           // the person's gestures sent with the message (forge 15d)
	debug        bool                             // the conversation's Debug setting: log its AI calls in detail
	workspace    *conversation.WorkspaceKnowledge // the workspace it was sent from (forge 16c); nil without one
}

// Send stores the message as a new round and runs the round's mode graph
// in the background (forge 06i): the graph gathers the context, builds the
// prompt and tools, streams the reply through the round's host, and
// follows it up. Send keeps the storage, the stream, approvals and the
// round's accounting.
func (s *service) Send(args conversationservice.SendArgs) *conversationservice.SendRequest {
	events := make(chan conversationservice.SendEvent, 64)
	r, failed := s.start(args)
	if failed != nil {
		events <- conversationservice.SendEvent{Kind: conversationservice.EventFailed, Common: *failed}
		close(events)
		return &conversationservice.SendRequest{Events: events}
	}
	go s.run(args, r, events)
	return &conversationservice.SendRequest{Events: events}
}

// start validates the message, resolves its mode and level, and stores the
// round.
func (s *service) start(args conversationservice.SendArgs) (*round, *features.ServiceResponse) {
	if failed := check(args.ServiceArgs, "conversation.write", args.ConversationID); failed != nil {
		return nil, failed
	}
	text := strings.TrimSpace(args.Text)
	switch {
	case text == "":
		return nil, args.Fail(http.StatusBadRequest, log.Code("empty_message").New("the message is empty"))
	case len(text) > maxText:
		return nil, args.Fail(http.StatusBadRequest, log.Code("message_too_long").Errorf("the message is longer than %d KB", maxText>>10))
	case len(args.Workbench) > maxWorkbench:
		return nil, args.Fail(http.StatusBadRequest, log.Code("workbench_too_large").Errorf("the workbench snapshot is larger than %d KB", maxWorkbench>>10))
	case len(args.Workbench) > 0 && !json.Valid(args.Workbench):
		return nil, args.Fail(http.StatusBadRequest, log.Code("bad_workbench").New("the workbench snapshot is not valid JSON"))
	}
	if s.chat == nil || s.config == nil || s.engine == nil {
		return nil, args.Fail(http.StatusServiceUnavailable, log.Code("chat_not_configured").New("chat is not configured"))
	}
	if failed := checkClientTools(args); failed != nil {
		return nil, failed
	}

	r := &round{}
	var failedWs *features.ServiceResponse
	if r.workspace, failedWs = s.workspaceOf(args); failedWs != nil {
		return nil, failedWs
	}
	var err error
	if r.chat, err = config.Read[conversationsettings.Chat](s.config, args.ServiceArgs, conversationsettings.ID); err != nil {
		return nil, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if r.levels, err = config.Read[aisettings.Levels](s.config, args.ServiceArgs, aisettings.LevelsID); err != nil {
		return nil, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	var ok bool
	if r.level, ok = r.levels.Find(args.Level); !ok {
		return nil, args.Fail(http.StatusBadRequest, log.Code("unknown_level").With("level", args.Level).Errorf("there is no chat level %q", args.Level))
	}
	if r.models, err = config.Read[aisettings.Models](s.config, args.ServiceArgs, aisettings.ModelsID); err != nil {
		return nil, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if r.model, ok = r.models.ByID(r.level.ModelRef); !ok {
		return nil, args.Fail(http.StatusBadRequest, log.Code("unknown_model").With("level", r.level.ID, "model", r.level.ModelRef).
			Errorf("level %q uses %q, which is not in the model catalog", r.level.ID, r.level.ModelRef))
	}

	if args.ConversationID != 0 {
		var failed *features.ServiceResponse
		if r.conversation, failed = s.find(args.ServiceArgs, args.ConversationID); failed != nil {
			return nil, failed
		}
		// Undo of a pre-mode switch runs the message again (forge 15f): the
		// round it replaces leaves the history, as a 🚫 one does
		if args.Undoes != 0 {
			if fb := s.Feedback(conversationservice.FeedbackArgs{ServiceArgs: args.ServiceArgs, RoundID: args.Undoes, Feedback: retrieval.FeedbackIgnore}); fb.Common.Code >= 300 {
				return nil, &fb.Common
			}
		}
		q := s.owned(args.ServiceArgs).Where("conversation_id = ? AND status IN ? AND reply <> '' AND feedback <> ?", args.ConversationID, []int{http.StatusOK, conversation.StatusStopped}, retrieval.FeedbackIgnore).Order("id DESC")
		if r.chat.HistoryRounds > 0 {
			q = q.Limit(r.chat.HistoryRounds)
		}
		if err := q.Find(&r.history).Error; err != nil {
			return nil, args.Fail(http.StatusInternalServerError, log.Wrap(err))
		}
		slices.Reverse(r.history)
	} else {
		r.conversation = conversation.Conversation{Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal)}
	}
	var failed *features.ServiceResponse
	if r.mode, failed = s.modeOf(args, r.conversation); failed != nil {
		return nil, failed
	}

	// The settings sent replace the conversation's, provider by provider
	r.settings = map[string]json.RawMessage{}
	if r.conversation.ContextSettings != "" {
		_ = json.Unmarshal([]byte(r.conversation.ContextSettings), &r.settings)
	}
	for id, v := range args.ContextSettings {
		r.settings[id] = v
	}
	stored, _ := json.Marshal(r.settings)
	r.conversation.ContextSettings = string(stored)
	r.conversation.Mode = r.mode.ID
	previous := r.conversation.LastRoundAt
	r.conversation.LastRoundAt = time.Now()

	r.activity = s.activityOf(args, r, previous)
	r.round = conversation.Round{
		Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal),
		Text: text, Level: r.level.ID, ModelName: r.model.Model, Workbench: string(args.Workbench), Mode: r.mode.ID,
	}
	if len(r.activity) > 0 {
		raw, _ := json.Marshal(r.activity)
		r.round.Activity = string(raw)
	}
	err = s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&r.conversation).Error; err != nil {
			return err
		}
		r.round.ConversationID = r.conversation.ID
		return tx.Create(&r.round).Error
	})
	if err != nil {
		return nil, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return r, nil
}

// modeOf is the mode a round runs in: the one sent, else the
// conversation's, else the default of the chat.modes settings.
func (s *service) modeOf(args conversationservice.SendArgs, c conversation.Conversation) (conversationsettings.Mode, *features.ServiceResponse) {
	modes, err := conversationsettings.ReadModes(s.config, args.ServiceArgs)
	if err != nil {
		return conversationsettings.Mode{}, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if args.Mode != "" {
		m, ok := modes.Find(args.Mode)
		if !ok {
			return m, args.Fail(http.StatusBadRequest, log.Code("unknown_mode").With("mode", args.Mode).Errorf("there is no chat mode %q", args.Mode))
		}
		return m, nil
	}
	if m, ok := modes.Find(c.Mode); ok {
		return m, nil
	}
	m, ok := modes.Default()
	if !ok {
		return m, args.Fail(http.StatusServiceUnavailable, log.Code("no_chat_modes").New("there are no chat modes: add one in Settings › AI › Chat modes"))
	}
	return m, nil
}

// run runs the round's mode graph from On user message, then completes the
// round if the graph did not: with what it said, else its failure.
func (s *service) run(args conversationservice.SendArgs, r *round, events chan<- conversationservice.SendEvent) {
	defer close(events)
	// Every retrieval the round makes, inline graphs and tools included,
	// leaves out the Volumes its workspace unchecked (forge 16c)
	if r.workspace != nil {
		args.Ctx = retrieval.WithScope(args.Context(), retrieval.Scope{ExceptVolumeIDs: r.workspace.ExceptVolumeIDs})
	}
	r.debug = debugging(s.settingsOf(args, r, conversation.DebugID, nil))
	h := newHost(s, args, r, events)
	conv, started := r.conversation, r.round
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventStarted, Conversation: &conv, Round: &started})
	// The pre-mode may switch the round's mode, or ask, before it runs
	s.preMode(args, r, h)
	if args.Context().Err() != nil {
		h.finish(nil) // stopped while it asked
		return
	}

	// The run outlives the browser: what follows the reply is done even
	// when nobody is left to tell. The reply itself stops with the browser.
	call := args.ServiceArgs
	call.Ctx = context.WithoutCancel(args.Context())
	call.Meta = features.Meta{Feature: "chat", Level: r.level.ID, ConversationID: r.conversation.ID, RoundID: r.round.ID, Debug: r.debug}
	all := map[string]json.RawMessage{}
	for _, p := range conversation.ContextProviders.All(s.registry) {
		all[p.ID] = s.settingsOf(args, r, p.ID, p.Defaults)
	}
	res, err := s.engine.Run(engine.RunArgs{
		ServiceArgs: call, GraphID: r.mode.GraphRef, Event: conversationnodes.EventUserMessage, Trigger: "chat", Host: h, Wall: roundWall,
		Inputs: cognition.Values{
			conversationnodes.PinText: r.round.Text, conversationnodes.PinWindow: r.history, conversationnodes.PinSettings: all,
			conversationnodes.PinWorkbench: json.RawMessage(orEmpty(args.Workbench)), conversationnodes.PinConversation: r.conversation,
			conversationnodes.PinTitle: r.conversation.Title, conversationnodes.PinLevel: r.level.ID, conversationnodes.PinRound: r.round,
			conversationnodes.PinActivity: orNone(r.activity),
		},
	})
	if err != nil {
		log.With("roundId", r.round.ID, "mode", r.mode.ID, "runId", res.RunID).Warn(call.Context(), "the mode graph %s failed: %v", r.mode.GraphRef, err)
	}
	h.finish(err)
}

// activityOf is the person's gestures to send with the message, read
// once (forge 15d): those since the conversation's previous round, or
// since the look-back of the activity settings for a first message,
// without the dismissed ones. Nothing is read while the conversation's
// activity is off; a failed read sends none.
func (s *service) activityOf(args conversationservice.SendArgs, r *round, previous time.Time) []conversation.Gesture {
	if s.activity == nil {
		return nil
	}
	var defaults json.RawMessage
	for _, p := range conversation.ContextProviders.All(s.registry) {
		if p.ID == conversation.ActivityID {
			defaults = p.Defaults
		}
	}
	set := conversation.ActivitySettingsOf(s.settingsOf(args, r, conversation.ActivityID, defaults))
	if !set.On() {
		return nil
	}
	after := time.Now().Add(-set.LookBack())
	if !previous.IsZero() {
		after = previous
	}
	gs, _, err := s.activity(args.ServiceArgs, after)
	if err != nil {
		log.Warn(args.Context(), "cannot read the recent activity, so the round goes without it: %v", err)
		return nil
	}
	return gs
}

// orNone is a list of gestures, never nil, for the activity pin.
func orNone(gs []conversation.Gesture) []conversation.Gesture {
	if gs == nil {
		return []conversation.Gesture{}
	}
	return gs
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// complete stores the finished round and sends done, then runs the
// chat.roundEnd hooks.
func (s *service) complete(args conversationservice.SendArgs, done *conversation.Round, emit func(conversationservice.SendEvent)) {
	persist := context.WithoutCancel(args.Context())
	if err := s.db.WithContext(persist).Save(done).Error; err != nil {
		log.With("roundId", done.ID).Error(persist, err, "cannot store the reply of round %d", done.ID)
	}
	finished := *done
	emit(conversationservice.SendEvent{Kind: conversationservice.EventDone, Round: &finished})
	s.end(args, finished)
}

// end runs the chat.roundEnd hooks, on a context the browser cannot cancel.
func (s *service) end(args conversationservice.SendArgs, round conversation.Round) {
	call := args.ServiceArgs
	call.Ctx = context.WithoutCancel(args.Context())
	call.Meta = features.Meta{ConversationID: round.ConversationID, RoundID: round.ID}
	for _, e := range conversation.RoundEnds.All(s.registry) {
		if e.End != nil {
			e.End(call, round)
		}
	}
}

// storeSegments records what the round cites, so its sources, Context
// inspector and feedback outlive the stream. A failed write is logged: the
// reply still goes ahead, without them.
func (s *service) storeSegments(args conversationservice.SendArgs, r *round, cited []retrieval.Scored) []retrieval.RoundSegment {
	if len(cited) == 0 {
		return nil
	}
	segments := make([]retrieval.RoundSegment, len(cited))
	for i, c := range cited {
		segments[i] = retrieval.RoundSegmentFrom(r.round.ID, r.round.Tenanted, c)
	}
	persist := context.WithoutCancel(args.Context())
	if err := s.db.WithContext(persist).Create(&segments).Error; err != nil {
		log.With("roundId", r.round.ID).Error(persist, err, "cannot store the segments of round %d", r.round.ID)
	}
	return segments
}

// clip cuts s to at most n bytes, on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// workspaceOf reads the workspace the message was sent from (forge 16c):
// the one named, which must be the caller's own, else their Default; a
// Default not made yet has the defaults (every Volume, no entry checked).
func (s *service) workspaceOf(args conversationservice.SendArgs) (*conversation.WorkspaceKnowledge, *features.ServiceResponse) {
	if s.workspaces == nil {
		return nil, nil
	}
	id := strings.TrimSpace(args.Workspace)
	named := id != ""
	if !named {
		id = conversation.DefaultWorkspace
	}
	w, ok := s.workspaces(args.ServiceArgs, id)
	switch {
	case ok:
		return &w, nil
	case named && id != conversation.DefaultWorkspace:
		return nil, args.Fail(http.StatusNotFound, log.Code("workspace_not_found").With("workspace", id).New("you have no such workspace"))
	}
	return &conversation.WorkspaceKnowledge{ID: conversation.DefaultWorkspace}, nil
}
