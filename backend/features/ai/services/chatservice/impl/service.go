package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
)

type service struct {
	provider chatservice.Provider
	config   config.Reader // the levels and the model catalog
}

// input validates args, applies its level and resolves the defaults for the
// provider.
func (s *service) input(args *chatservice.CompleteArgs) (context.Context, chatservice.ChatInput, *features.ServiceResponse) {
	if s.provider == nil {
		return nil, chatservice.ChatInput{}, args.Fail(providers.ErrNotConfigured.Code, providers.Wrap(providers.ErrNotConfigured))
	}
	if len(args.Messages) == 0 {
		return nil, chatservice.ChatInput{}, args.Fail(http.StatusBadRequest, log.Code("no_messages").New("no messages"))
	}
	for i, m := range args.Messages {
		switch m.Role {
		case chatservice.RoleUser, chatservice.RoleAssistant:
		case chatservice.RoleTool:
			if i == 0 || len(args.Messages[i-1].ToolCalls) == 0 {
				return nil, chatservice.ChatInput{}, args.Fail(http.StatusBadRequest, log.Code("orphan_tool_results").New("tool results must follow an assistant message with tool calls"))
			}
		default:
			return nil, chatservice.ChatInput{}, args.Fail(http.StatusBadRequest, log.Code("unknown_role").Errorf("unknown role %s", m.Role))
		}
	}
	if last := args.Messages[len(args.Messages)-1]; last.Role != chatservice.RoleUser && last.Role != chatservice.RoleTool {
		return nil, chatservice.ChatInput{}, args.Fail(http.StatusBadRequest, log.Code("last_not_user").New("the last message must be the user's or tool results"))
	}
	for _, t := range args.Tools {
		if t.Name == "" || (len(t.Parameters) > 0 && !json.Valid(t.Parameters)) {
			return nil, chatservice.ChatInput{}, args.Fail(http.StatusBadRequest, log.Code("invalid_tool").With("tool", t.Name).New("a tool needs a name and a valid JSON Schema"))
		}
	}
	if len(args.JSONSchema) > 0 && !json.Valid(args.JSONSchema) {
		return nil, chatservice.ChatInput{}, args.Fail(http.StatusBadRequest, log.Code("invalid_json_schema").New("jsonSchema is not valid JSON"))
	}

	in := chatservice.ChatInput{
		Model:       args.Model,
		System:      args.System,
		Messages:    args.Messages,
		Temperature: args.Temperature,
		MaxTokens:   args.MaxTokens,
		JSONSchema:  args.JSONSchema,
		Tools:       args.Tools,
		ToolChoice:  args.ToolChoice,
	}
	if args.Level != "" {
		if failed := s.applyLevel(args, &in); failed != nil {
			return nil, chatservice.ChatInput{}, failed
		}
	}
	if in.Model == "" {
		in.Model = s.provider.ChatModel()
	}
	return args.CallContext(), in, nil
}

// applyLevel fills what args left unset from the level's settings, and
// attributes the call to the level.
func (s *service) applyLevel(args *chatservice.CompleteArgs, in *chatservice.ChatInput) *features.ServiceResponse {
	if s.config == nil {
		return args.Fail(http.StatusServiceUnavailable, log.Code("no_levels").New("chat levels are not configured"))
	}
	levels, err := config.Read[aisettings.Levels](s.config, args.ServiceArgs, aisettings.LevelsID)
	if err != nil {
		return args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	level, ok := levels.Find(args.Level)
	if !ok {
		return args.Fail(http.StatusBadRequest, log.Code("unknown_level").With("level", args.Level).Errorf("there is no chat level %q", args.Level))
	}
	models, err := config.Read[aisettings.Models](s.config, args.ServiceArgs, aisettings.ModelsID)
	if err != nil {
		return args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	model, ok := models.ByID(level.ModelRef)
	if !ok {
		return args.Fail(http.StatusBadRequest, log.Code("unknown_model").With("level", level.ID, "model", level.ModelRef).
			Errorf("level %q uses %q, which is not in the model catalog", level.ID, level.ModelRef))
	}
	if in.Model == "" {
		in.Model = model.Model
	}
	if in.Temperature == nil && level.Temperature != nil {
		t := float32(*level.Temperature)
		in.Temperature = &t
	}
	if in.MaxTokens == nil && level.MaxOutputTokens > 0 {
		in.MaxTokens = &level.MaxOutputTokens
	}
	in.ThinkingBudget = level.ThinkingBudget
	args.Meta.Level = level.ID
	return nil
}

func response(args chatservice.CompleteArgs, in chatservice.ChatInput, out chatservice.ChatOutput) chatservice.CompleteResponse {
	return chatservice.CompleteResponse{
		Common:       features.OK(),
		Message:      chatservice.Message{Role: chatservice.RoleAssistant, Content: out.Content, ToolCalls: out.ToolCalls},
		Model:        in.Model,
		Level:        args.Meta.Level,
		FinishReason: out.FinishReason,
		Usage:        out.Usage,
	}
}

// debugInput logs how a debugged call was resolved: what it is for, its
// level and model, and the settings the provider gets.
func debugInput(args chatservice.CompleteArgs, in chatservice.ChatInput, stream bool) {
	if !args.Meta.Debug {
		return
	}
	roles := make([]string, len(in.Messages))
	for i, m := range in.Messages {
		roles[i] = string(m.Role)
	}
	tools := make([]string, len(in.Tools))
	for i, t := range in.Tools {
		tools[i] = t.Name
	}
	log.With("feature", args.Meta.Feature, "level", args.Meta.Level, "model", in.Model, "stream", stream,
		"temperature", deref(in.Temperature), "maxTokens", deref(in.MaxTokens), "thinkingBudget", deref(in.ThinkingBudget),
		"messages", strings.Join(roles, ","), "tools", strings.Join(tools, ","), "toolChoice", in.ToolChoice, "jsonSchemaBytes", len(in.JSONSchema)).
		Info(args.Context(), "chat call for %q at level %q: model %s, temperature %s, max tokens %s, thinking budget %s, %d messages, %d tools",
			args.Meta.Feature, args.Meta.Level, in.Model, deref(in.Temperature), deref(in.MaxTokens), deref(in.ThinkingBudget), len(in.Messages), len(in.Tools))
}

// debugFailed logs a debugged call's failure at warn, since Fail logs a 4xx
// only at debug.
func debugFailed(args chatservice.CompleteArgs, failed *features.ServiceResponse) {
	if !args.Meta.Debug {
		return
	}
	log.With("feature", args.Meta.Feature, "level", args.Meta.Level, "status", failed.Code, "errorCode", failed.ErrorCode).
		Warn(args.Context(), "chat call for %q at level %q failed with %d %s: %s", args.Meta.Feature, args.Meta.Level, failed.Code, failed.ErrorCode, failed.Message)
}

// deref is a setting for the logs: its value, or "default" when unset.
func deref[T any](v *T) string {
	if v == nil {
		return "default"
	}
	return fmt.Sprint(*v)
}

var log = providers.Log.Tags("chat")
