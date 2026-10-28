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
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"gorm.io/gorm"
)

const (
	maxText      = 32 << 10 // a message
	maxWorkbench = 16 << 10 // a workbench snapshot; the browser keeps it near 4 KB
)

// round is what Send has resolved and stored before the reply starts.
type round struct {
	conversation conversation.Conversation
	round        conversation.Round
	chat         conversationsettings.Chat
	levels       aisettings.Levels
	level        aisettings.Level
	model        aisettings.Model
	history      []conversation.Round       // the window: the rounds sent as history, oldest first
	settings     map[string]json.RawMessage // the context settings, by provider ID
	segments     []retrieval.RoundSegment   // what the round injected, once stored
	debug        bool                       // the conversation's Debug setting: log its AI calls in detail
}

// Send stores the message as a new round and streams the reply in the
// background. The window (the conversation's last historyRounds rounds) is
// sent as history; memory comes from the context providers, such as
// Chronicle's, and the round followers update it after the reply.
func (s *service) Send(args conversationservice.SendArgs) *conversationservice.SendRequest {
	events := make(chan conversationservice.SendEvent, 64)
	r, failed := s.start(args)
	if failed != nil {
		events <- conversationservice.SendEvent{Kind: conversationservice.EventFailed, Common: *failed}
		close(events)
		return &conversationservice.SendRequest{Events: events}
	}
	go s.reply(args, r, events)
	return &conversationservice.SendRequest{Events: events}
}

// start validates the message, resolves its level and stores the round.
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
	if s.chat == nil || s.config == nil {
		return nil, args.Fail(http.StatusServiceUnavailable, log.Code("chat_not_configured").New("chat is not configured"))
	}
	if failed := checkClientTools(args); failed != nil {
		return nil, failed
	}

	r := &round{}
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
	models, err := config.Read[aisettings.Models](s.config, args.ServiceArgs, aisettings.ModelsID)
	if err != nil {
		return nil, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if r.model, ok = models.ByID(r.level.ModelRef); !ok {
		return nil, args.Fail(http.StatusBadRequest, log.Code("unknown_model").With("level", r.level.ID, "model", r.level.ModelRef).
			Errorf("level %q uses %q, which is not in the model catalog", r.level.ID, r.level.ModelRef))
	}

	if args.ConversationID != 0 {
		var failed *features.ServiceResponse
		if r.conversation, failed = s.find(args.ServiceArgs, args.ConversationID); failed != nil {
			return nil, failed
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
	r.conversation.LastRoundAt = time.Now()

	r.round = conversation.Round{
		Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal),
		Text: text, Level: r.level.ID, ModelName: r.model.Model, Workbench: string(args.Workbench),
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

// reply streams the reply and completes the round. The round is saved even
// when the user stops it, so its writes do not use the request's context.
func (s *service) reply(args conversationservice.SendArgs, r *round, events chan<- conversationservice.SendEvent) {
	defer close(events)
	r.debug = debugging(s.settingsOf(args, r, conversation.DebugID, nil))
	ctx := args.Context()
	emit := func(e conversationservice.SendEvent) {
		select {
		case events <- e:
		case <-ctx.Done():
		}
	}
	conv, started := r.conversation, r.round
	emit(conversationservice.SendEvent{Kind: conversationservice.EventStarted, Conversation: &conv, Round: &started})

	start := time.Now()
	messages := make([]chatservice.Message, 0, 2*len(r.history)+1)
	for _, h := range r.history {
		messages = append(messages, chatservice.Message{Role: chatservice.RoleUser, Content: h.Text}, chatservice.Message{Role: chatservice.RoleAssistant, Content: h.Reply})
	}
	messages = append(messages, chatservice.Message{Role: chatservice.RoleUser, Content: r.round.Text})
	call := args.ServiceArgs
	call.Meta = features.Meta{Feature: "chat", Level: r.level.ID, ConversationID: r.conversation.ID, RoundID: r.round.ID, Debug: r.debug}
	system, cited := prompt(r.chat.SystemPrompt, s.gather(args, r))
	if r.segments = s.storeSegments(args, r, cited); len(r.segments) > 0 {
		emit(conversationservice.SendEvent{Kind: conversationservice.EventSources, Segments: r.segments})
	}
	offered, toolSettings := s.offered(args, r)
	if len(offered) > 0 {
		system += toolsPrompt
	}
	// The reply is the tool loop: with no tools, one streamed call
	c := &citer{s: s, args: args, r: r, emit: emit}
	out := tools.Loop{DB: s.db, Chat: s.chat}.Run(tools.RunArgs{
		CompleteArgs: chatservice.CompleteArgs{ServiceArgs: call, Level: r.level.ID, System: system, Messages: messages},
		Tools:        offered,
		Settings:     toolSettings,
		Allowed:      allowed(r),
		Gate:         gate{s: s, args: args, r: r},
		Stream:       true,
		OnDelta: func(text string) {
			emit(conversationservice.SendEvent{Kind: conversationservice.EventDelta, Text: text})
		},
		OnCall: func(tc tools.ToolCall) {
			if tc.Status == tools.StatusAwaiting || tc.Status == tools.StatusClient {
				s.expect(args.Principal, tc.RoundID, tc.CallID)
			}
			emit(conversationservice.SendEvent{Kind: conversationservice.EventTool, ToolCall: &tc})
		},
		Cite: c.cite,
	})
	resp := out.Response

	done := &r.round
	done.LatencyMs = time.Since(start).Milliseconds()
	done.Reply = out.Reply
	// Every step is priced as usage.Recorder prices its record
	var tokens aisettings.Tokens
	for _, u := range out.Usage {
		t := aisettings.Tokens{Input: u.InputTokens, Cached: u.CachedTokens, Output: u.OutputTokens, Thinking: u.ThinkingTokens}
		done.CostMicros += r.model.Pricing.CostMicros(t)
		tokens.Input, tokens.Cached, tokens.Output, tokens.Thinking = tokens.Input+t.Input, tokens.Cached+t.Cached, tokens.Output+t.Output, tokens.Thinking+t.Thinking
	}
	done.InputTokens, done.CachedTokens, done.OutputTokens, done.ThinkingTokens = tokens.Input, tokens.Cached, tokens.Output, tokens.Thinking
	if done.Currency = r.model.Pricing.Currency; done.Currency == "" {
		done.Currency = "USD"
	}
	switch {
	case ctx.Err() != nil:
		done.Status, done.FinishReason = conversation.StatusStopped, "stopped"
	case resp.Common.Code >= 300:
		done.Status, done.Error = resp.Common.Code, resp.Common.Message
	default:
		done.Status, done.FinishReason, done.ModelName = http.StatusOK, resp.FinishReason, resp.Model
	}
	persist := context.WithoutCancel(ctx)
	if err := s.db.WithContext(persist).Save(done).Error; err != nil {
		log.With("roundId", done.ID).Error(persist, err, "cannot store the reply of round %d", done.ID)
	}
	finished := *done
	emit(conversationservice.SendEvent{Kind: conversationservice.EventDone, Round: &finished})

	if r.conversation.Title == "" && done.Status == http.StatusOK && ctx.Err() == nil {
		if title := s.title(args.ServiceArgs, r); title != "" {
			r.conversation.Title = title
			if err := s.db.WithContext(persist).Model(&r.conversation).Update("title", title).Error; err != nil {
				log.With("conversationId", r.conversation.ID).Error(persist, err, "cannot store the title of conversation %d", r.conversation.ID)
			}
			titled := r.conversation
			emit(conversationservice.SendEvent{Kind: conversationservice.EventTitle, Conversation: &titled})
		}
	}
	if done.Status == http.StatusOK && ctx.Err() == nil {
		s.follow(args, r, emit)
	}
}

// follow runs the round followers, in order, on a context the browser
// cannot cancel, and reports what each did.
func (s *service) follow(args conversationservice.SendArgs, r *round, emit func(conversationservice.SendEvent)) {
	call := args.ServiceArgs
	call.Ctx = context.WithoutCancel(args.Context())
	call.Meta = features.Meta{ConversationID: r.conversation.ID, RoundID: r.round.ID, Debug: r.debug}
	for _, f := range conversation.RoundFollowers.All(s.registry) {
		if f.Follow == nil {
			continue
		}
		followed, err := f.Follow(conversation.FollowArgs{
			ServiceArgs: call, Conversation: r.conversation, Round: r.round,
			Settings: s.settingsOf(args, r, f.ID, nil), Segments: r.segments,
		})
		if err != nil {
			log.With("follower", f.ID, "roundId", r.round.ID).Warn(call.Context(), "round follower %s failed: %v", f.ID, err)
			continue
		}
		if followed != nil {
			emit(conversationservice.SendEvent{Kind: conversationservice.EventFollowed, Follower: f.ID, Followed: followed})
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

const titlePrompt = "You name conversations. Reply with a title of at most six words for the conversation below: no quotes, no punctuation at the end, nothing else."

// title asks the title level for the conversation's title after its first
// round; the empty string when that fails.
func (s *service) title(args features.ServiceArgs, r *round) string {
	level, ok := r.levels.Find(r.chat.TitleLevel)
	if !ok {
		level = r.level
	}
	call := args
	call.Meta = features.Meta{Feature: "chat-title", Level: level.ID, ConversationID: r.conversation.ID, Debug: r.debug}
	resp := s.chat.Complete(chatservice.CompleteArgs{
		ServiceArgs: call,
		Level:       level.ID,
		System:      titlePrompt,
		Messages:    []chatservice.Message{{Role: chatservice.RoleUser, Content: "User: " + clip(r.round.Text, 2000) + "\n\nAssistant: " + clip(r.round.Reply, 2000)}},
	})
	if resp.Common.Code != http.StatusOK {
		return "" // Fail has logged it
	}
	return cleanTitle(resp.Message.Content)
}

// cleanTitle keeps the first line of a model's title, without the quotes
// and markdown models add anyway.
func cleanTitle(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.Trim(strings.TrimSpace(s), "\"'`*#. ")
	return clip(s, 80)
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
