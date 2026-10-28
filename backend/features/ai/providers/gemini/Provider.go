// Package gemini is the Google Gemini API provider for the ai services.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"google.golang.org/genai"
)

// Defaults; each can be overridden with an Option.
const (
	DefaultEmbedModel      = "gemini-embedding-001"
	DefaultEmbedDimensions = 768
	DefaultChatModel       = "gemini-3.8-flash"
)

// maxBatch is how many texts the Gemini API embeds in one request.
const maxBatch = 100

// nativeDimensions is the size Gemini returns already normalized; smaller
// (truncated) vectors must be normalized by the caller.
const nativeDimensions = 3072

// Provider implements embedservice.Provider and chatservice.Provider.
type Provider struct {
	apiKey     string
	embedModel string
	dimensions int
	chatModel  string

	// keyFor, when set, picks each call's key from its context, so a user's
	// or tenant's own key is used for their calls (Secrets question 7)
	keyFor func(ctx context.Context) string

	mu      sync.Mutex
	clients map[string]*genai.Client // by key, made on first use
}

// maxClients bounds the clients kept, one per key in use.
const maxClients = 64

// Option configures the provider created by New.
type Option func(*Provider)

// WithAPIKey sets the Gemini API key; the default is $GEMINI_API_KEY. serve
// sets it from the ai.providers settings instead, with SetAPIKey.
func WithAPIKey(key string) Option {
	return func(p *Provider) {
		p.apiKey = key
	}
}

func WithEmbedModel(model string) Option {
	return func(p *Provider) {
		p.embedModel = model
	}
}

// WithEmbedDimensions sets the embedding size (gemini-embedding-001 supports
// 128 to 3072; 768, 1536 and 3072 are recommended).
func WithEmbedDimensions(n int) Option {
	return func(p *Provider) {
		p.dimensions = n
	}
}

// WithKeyFor picks the API key of each call from its context (whose
// principal features.CallFrom reads); an empty key fails the call as not
// configured. It replaces the provider's own key.
func WithKeyFor(keyFor func(ctx context.Context) string) Option {
	return func(p *Provider) {
		p.keyFor = keyFor
	}
}

func WithChatModel(model string) Option {
	return func(p *Provider) {
		p.chatModel = model
	}
}

// New returns a provider. Without an API key it is still returned, and every
// call fails with providers.ErrNotConfigured, so the server runs without AI.
func New(opts ...Option) *Provider {
	p := &Provider{
		apiKey:     os.Getenv("GEMINI_API_KEY"),
		embedModel: DefaultEmbedModel,
		dimensions: DefaultEmbedDimensions,
		chatModel:  DefaultChatModel,
		clients:    map[string]*genai.Client{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Configured reports whether the provider has an API key for calls with
// no principal, such as indexing.
func (p *Provider) Configured() bool {
	return p.key(context.Background()) != ""
}

// SetAPIKey replaces the provider's own API key; an empty key leaves the
// provider unconfigured. It is not used with WithKeyFor.
func (p *Provider) SetAPIKey(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.apiKey = key
}

func (p *Provider) key(ctx context.Context) string {
	if p.keyFor != nil {
		return p.keyFor(ctx)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.apiKey
}

func (p *Provider) getClient(ctx context.Context) (*genai.Client, error) {
	key := p.key(ctx)
	if key == "" {
		return nil, providers.ErrNotConfigured
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if client, ok := p.clients[key]; ok {
		return client, nil
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, providers.Errorf(http.StatusServiceUnavailable, "gemini: %s", err)
	}
	if len(p.clients) >= maxClients {
		clear(p.clients) // keys replaced over time; a client is cheap to make again
	}
	p.clients[key] = client
	return client, nil
}

func (p *Provider) EmbedModel() embedservice.ModelInfo {
	return embedservice.ModelInfo{Model: p.embedModel, Dimensions: p.dimensions, MaxBatch: maxBatch}
}

func (p *Provider) Embed(ctx context.Context, in embedservice.EmbedInput) (embedservice.EmbedOutput, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return embedservice.EmbedOutput{}, err
	}
	contents := make([]*genai.Content, len(in.Texts))
	for i, text := range in.Texts {
		contents[i] = genai.NewContentFromText(text, genai.RoleUser)
	}
	config := &genai.EmbedContentConfig{TaskType: "RETRIEVAL_DOCUMENT"}
	if in.Purpose == embedservice.PurposeQuery {
		config.TaskType = "RETRIEVAL_QUERY"
	} else {
		config.Title = in.Title
	}
	dims := p.dimensions
	if dims > 0 {
		d := int32(dims)
		config.OutputDimensionality = &d
	}

	resp, err := client.Models.EmbedContent(ctx, in.Model, contents, config)
	if err != nil {
		return embedservice.EmbedOutput{}, wrap(err)
	}
	out := embedservice.EmbedOutput{Vectors: make([][]float32, len(resp.Embeddings))}
	for i, e := range resp.Embeddings {
		v := e.Values
		if len(v) != nativeDimensions {
			v = normalize(v)
		}
		out.Vectors[i] = v
		if e.Statistics != nil {
			out.Usage.InputTokens += int(e.Statistics.TokenCount)
		}
	}
	return out, nil
}

func (p *Provider) ChatModel() string {
	return p.chatModel
}

func (p *Provider) Complete(ctx context.Context, in chatservice.ChatInput) (chatservice.ChatOutput, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return chatservice.ChatOutput{}, err
	}
	contents, config, err := request(in)
	if err != nil {
		return chatservice.ChatOutput{}, err
	}
	debugRequest(ctx, in.Model, contents, config)
	start := time.Now()
	resp, err := client.Models.GenerateContent(ctx, in.Model, contents, config)
	if err != nil {
		debugFailure(ctx, in.Model, start, err)
		return chatservice.ChatOutput{}, wrap(err)
	}
	out := chatservice.ChatOutput{Content: resp.Text()}
	collect(&out, resp)
	out.ToolCalls = toolCalls(resp)
	debugReply(ctx, in.Model, start, out)
	return out, nil
}

func (p *Provider) Stream(ctx context.Context, in chatservice.ChatInput, onDelta func(chatservice.Delta) error) (chatservice.ChatOutput, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return chatservice.ChatOutput{}, err
	}
	contents, config, err := request(in)
	if err != nil {
		return chatservice.ChatOutput{}, err
	}
	debugRequest(ctx, in.Model, contents, config)
	start := time.Now()
	var out chatservice.ChatOutput
	var text strings.Builder
	for resp, err := range client.Models.GenerateContentStream(ctx, in.Model, contents, config) {
		if err != nil {
			debugFailure(ctx, in.Model, start, err)
			return chatservice.ChatOutput{}, wrap(err)
		}
		if piece := resp.Text(); piece != "" {
			text.WriteString(piece)
			if err := onDelta(chatservice.Delta{Text: piece}); err != nil {
				return chatservice.ChatOutput{}, err
			}
		}
		collect(&out, resp)
		out.ToolCalls = append(out.ToolCalls, toolCalls(resp)...)
	}
	out.Content = text.String()
	debugReply(ctx, in.Model, start, out)
	return out, nil
}

// request maps a chat input to Gemini's contents and config. Tool results
// go back as the user's function responses, and the model's calls keep
// their thought signatures, which Gemini needs to continue its reasoning.
func request(in chatservice.ChatInput) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	contents := make([]*genai.Content, 0, len(in.Messages))
	for _, m := range in.Messages {
		switch m.Role {
		case chatservice.RoleAssistant:
			var parts []*genai.Part
			if m.Content != "" {
				parts = append(parts, genai.NewPartFromText(m.Content))
			}
			for _, c := range m.ToolCalls {
				var args map[string]any
				if len(c.Args) > 0 {
					if err := json.Unmarshal(c.Args, &args); err != nil {
						return nil, nil, providers.Errorf(http.StatusBadRequest, "tool call %s: %s", c.Name, err)
					}
				}
				parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: c.ID, Name: c.Name, Args: args}, ThoughtSignature: c.Signature})
			}
			if len(parts) == 0 {
				parts = append(parts, genai.NewPartFromText(""))
			}
			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleModel))
		case chatservice.RoleTool:
			parts := make([]*genai.Part, len(m.Results))
			for i, r := range m.Results {
				parts[i] = &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: r.CallID, Name: r.Name, Response: responseOf(r.Result)}}
			}
			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleUser))
		default:
			contents = append(contents, genai.NewContentFromText(m.Content, genai.RoleUser))
		}
	}
	config := &genai.GenerateContentConfig{Temperature: in.Temperature}
	if in.System != "" {
		config.SystemInstruction = genai.NewContentFromText(in.System, genai.RoleUser)
	}
	if in.MaxTokens != nil {
		config.MaxOutputTokens = int32(*in.MaxTokens)
	}
	if in.ThinkingBudget != nil {
		config.ThinkingConfig = thinking(in.Model, *in.ThinkingBudget)
	}
	if len(in.JSONSchema) > 0 {
		var schema any
		if err := json.Unmarshal(in.JSONSchema, &schema); err != nil {
			return nil, nil, providers.Errorf(http.StatusBadRequest, "jsonSchema: %s", err)
		}
		config.ResponseMIMEType = "application/json"
		config.ResponseJsonSchema = schema
	}
	if len(in.Tools) > 0 {
		decls := make([]*genai.FunctionDeclaration, len(in.Tools))
		for i, t := range in.Tools {
			decls[i] = &genai.FunctionDeclaration{Name: t.Name, Description: t.Description}
			if len(t.Parameters) > 0 {
				var schema any
				if err := json.Unmarshal(t.Parameters, &schema); err != nil {
					return nil, nil, providers.Errorf(http.StatusBadRequest, "tool %s: %s", t.Name, err)
				}
				decls[i].ParametersJsonSchema = schema
			}
		}
		config.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
		mode := genai.FunctionCallingConfigModeAuto
		switch in.ToolChoice {
		case chatservice.ToolChoiceNone:
			mode = genai.FunctionCallingConfigModeNone
		case chatservice.ToolChoiceRequired:
			mode = genai.FunctionCallingConfigModeAny
		}
		config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: mode}}
	}
	return contents, config, nil
}

// thinking is the config for a thinking budget. Gemini 3 models think in
// levels and refuse a budget of 0 as an invalid argument, so 0 asks them for
// their least thinking instead: the minimal level on Flash models, low on
// the others, which have no minimal level.
func thinking(model string, budget int) *genai.ThinkingConfig {
	model = strings.TrimPrefix(model, "models/")
	if budget == 0 && !strings.HasPrefix(model, "gemini-1") && !strings.HasPrefix(model, "gemini-2") {
		if strings.Contains(model, "flash") {
			return &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}
		}
		return &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}
	}
	b := int32(budget)
	return &genai.ThinkingConfig{ThinkingBudget: &b}
}

// responseOf is a tool result as Gemini's function response, which must be
// an object: anything else is wrapped as {"result": …}.
func responseOf(result json.RawMessage) map[string]any {
	var out map[string]any
	if json.Unmarshal(result, &out) == nil && out != nil {
		return out
	}
	var v any
	_ = json.Unmarshal(result, &v)
	return map[string]any{"result": v}
}

// toolCalls are the function calls a response (or stream chunk) carries.
func toolCalls(resp *genai.GenerateContentResponse) []chatservice.ToolCall {
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil
	}
	var calls []chatservice.ToolCall
	for _, part := range resp.Candidates[0].Content.Parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}
		args, _ := json.Marshal(part.FunctionCall.Args)
		if part.FunctionCall.Args == nil {
			args = []byte("{}")
		}
		calls = append(calls, chatservice.ToolCall{ID: part.FunctionCall.ID, Name: part.FunctionCall.Name, Args: args, Signature: part.ThoughtSignature})
	}
	return calls
}

// collect records the finish reason and usage a response (or stream chunk) carries.
func collect(out *chatservice.ChatOutput, resp *genai.GenerateContentResponse) {
	if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != "" {
		out.FinishReason = finishReason(resp.Candidates[0].FinishReason)
	}
	if u := resp.UsageMetadata; u != nil {
		out.Usage = chatservice.Usage{
			InputTokens:    int(u.PromptTokenCount),
			CachedTokens:   int(u.CachedContentTokenCount),
			OutputTokens:   int(u.CandidatesTokenCount),
			ThinkingTokens: int(u.ThoughtsTokenCount),
		}
	}
}

func finishReason(r genai.FinishReason) string {
	switch r {
	case genai.FinishReasonStop:
		return "stop"
	case genai.FinishReasonMaxTokens:
		return "length"
	case genai.FinishReasonSafety:
		return "safety"
	}
	return strings.ToLower(string(r))
}

// debugging reports whether the call's Meta asks for its details to be logged.
func debugging(ctx context.Context) bool {
	_, meta, ok := features.CallFrom(ctx)
	return ok && meta.Debug
}

// debugRequest logs what is sent to Gemini when the call is debugged: the
// config as JSON, without the system instruction's text, and the shape of
// the contents. Prompts and replies are never logged.
func debugRequest(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) {
	if !debugging(ctx) {
		return
	}
	shown := *config
	system := 0
	if config.SystemInstruction != nil {
		for _, part := range config.SystemInstruction.Parts {
			system += len(part.Text)
		}
		shown.SystemInstruction = nil
	}
	configJSON, _ := json.Marshal(&shown)
	shapes := make([]string, len(contents))
	for i, c := range contents {
		shapes[i] = shape(c)
	}
	log.With("model", model, "config", string(configJSON), "systemBytes", system, "contents", strings.Join(shapes, " | ")).
		Info(ctx, "gemini request to %s: %d contents, config %s", model, len(contents), configJSON)
}

// shape describes a content without its text: its role and each part's
// kind, with a text's length and a call's or response's function name.
func shape(c *genai.Content) string {
	parts := make([]string, 0, len(c.Parts))
	for _, part := range c.Parts {
		var desc string
		switch {
		case part == nil:
			desc = "nil"
		case part.FunctionCall != nil:
			desc = "call:" + part.FunctionCall.Name
		case part.FunctionResponse != nil:
			desc = "response:" + part.FunctionResponse.Name
		default:
			desc = fmt.Sprintf("text:%d", len(part.Text))
		}
		if part != nil && len(part.ThoughtSignature) > 0 {
			desc += "+signature"
		}
		parts = append(parts, desc)
	}
	return c.Role + "[" + strings.Join(parts, ",") + "]"
}

// debugFailure logs Gemini's whole error, with its status and details,
// when the call is debugged; the error users see carries its message only.
func debugFailure(ctx context.Context, model string, start time.Time, err error) {
	if !debugging(ctx) {
		return
	}
	b := log.With("model", model, "elapsedMs", time.Since(start).Milliseconds())
	if apiErr, ok := errors.AsType[genai.APIError](err); ok {
		details, _ := json.Marshal(apiErr.Details)
		b.With("httpStatus", apiErr.Code, "status", apiErr.Status, "details", string(details)).
			Warn(ctx, "gemini refused the request to %s: %d %s: %s; details %s", model, apiErr.Code, apiErr.Status, apiErr.Message, details)
		return
	}
	b.Warn(ctx, "gemini request to %s failed: %v", model, err)
}

// debugReply logs how a debugged call ended: its finish reason, usage and
// tool calls, without the reply's text.
func debugReply(ctx context.Context, model string, start time.Time, out chatservice.ChatOutput) {
	if !debugging(ctx) {
		return
	}
	names := make([]string, len(out.ToolCalls))
	for i, c := range out.ToolCalls {
		names[i] = c.Name
	}
	log.With("model", model, "elapsedMs", time.Since(start).Milliseconds(), "finishReason", out.FinishReason,
		"inputTokens", out.Usage.InputTokens, "outputTokens", out.Usage.OutputTokens, "thinkingTokens", out.Usage.ThinkingTokens,
		"replyBytes", len(out.Content), "toolCalls", strings.Join(names, ",")).
		Info(ctx, "gemini reply from %s: finish %q, %d input, %d output and %d thinking tokens, %d bytes, tool calls [%s]",
			model, out.FinishReason, out.Usage.InputTokens, out.Usage.OutputTokens, out.Usage.ThinkingTokens, len(out.Content), strings.Join(names, ","))
}

var log = providers.Log.Tags("gemini")

// wrap turns a Gemini API error into a providers.Error with a matching status.
func wrap(err error) error {
	if apiErr, ok := errors.AsType[genai.APIError](err); ok {
		return &providers.Error{Code: status(apiErr.Code), Message: "gemini: " + apiErr.Message}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &providers.Error{Code: http.StatusGatewayTimeout, Message: "gemini: " + err.Error()}
	}
	return &providers.Error{Code: http.StatusBadGateway, Message: "gemini: " + err.Error()}
}

// status keeps the client errors callers can act on and reports any other
// upstream failure as a bad gateway.
func status(code int) uint16 {
	switch code {
	case http.StatusBadRequest, http.StatusTooManyRequests, http.StatusNotFound:
		return uint16(code)
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusServiceUnavailable // our key is wrong, not the caller's request
	}
	return http.StatusBadGateway
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}
