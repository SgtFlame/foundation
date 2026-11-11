package connectors

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/tools"
)

// The mappings below offer a contract operation as a tool with nothing
// written by hand (07b › Tool-shaped metadata): to Axiom's own models, to
// an MCP client and to another vendor's function calling, and a whole
// contract as an OpenAPI document. Each is a pure function of the
// metadata, with a golden test over the test contract.

// SourceConnector is a tools.Spec's source when it is a contract
// operation (run through the dispatcher from iteration 15).
const SourceConnector = "connector"

// MCPRevision is the MCP specification revision MCPTool follows.
const MCPRevision = "2025-11-25"

// ToolEffect is an operation's effect as a tool's: read is read, write is
// write, and send reaches outside, so it asks like a write.
func ToolEffect(effect string) tools.Effect {
	switch effect {
	case EffectRead:
		return tools.EffectRead
	case EffectWrite:
		return tools.EffectWrite
	}
	return tools.EffectExternal
}

// ToolSpec is an operation as Axiom's tool sets carry it.
func ToolSpec(c Contract, op Operation) tools.Spec {
	return tools.Spec{Name: ToolName(c.ID, op.ID), Title: op.Title, Description: op.Description, Input: op.Args,
		Effect: ToolEffect(op.Effect), Source: SourceConnector}
}

// mcpTool is a Tool of the MCP specification (MCPRevision).
type mcpTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  mcpAnnotations  `json:"annotations"`
}

type mcpAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint bool   `json:"destructiveHint"`
	IdempotentHint  bool   `json:"idempotentHint"`
	OpenWorldHint   bool   `json:"openWorldHint"`
}

// MCPTool is an operation as an MCP tool object: its wire name, schemas
// (the result as outputSchema, for structuredContent) and the annotations
// its effect, destructive flag and key give.
func MCPTool(c Contract, op Operation) json.RawMessage {
	t := mcpTool{Name: WireName(ToolName(c.ID, op.ID)), Title: op.Title, Description: op.Description,
		InputSchema: op.Args, OutputSchema: op.Result,
		Annotations: mcpAnnotations{Title: op.Title, ReadOnlyHint: op.Effect == EffectRead,
			DestructiveHint: op.Effect == EffectWrite && op.Destructive, IdempotentHint: op.Key != "" || op.Effect == EffectRead,
			OpenWorldHint: op.Effect == EffectSend}}
	raw, _ := json.Marshal(t)
	return raw
}

// MCPError is a typed error as an MCP tool's failed result.
func MCPError(e *Error) json.RawMessage {
	body, _ := json.Marshal(e)
	raw, _ := json.Marshal(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": string(body)}}})
	return raw
}

// Vendors FunctionSpec knows.
const (
	VendorGemini    = "gemini"
	VendorAnthropic = "anthropic"
	VendorOpenAI    = "openai"
)

// FunctionSpec is an operation as a vendor's function declaration:
// Gemini's functionDeclarations entry, Claude's tool and OpenAI's
// function tool.
func FunctionSpec(c Contract, op Operation, vendor string) (json.RawMessage, error) {
	name := WireName(ToolName(c.ID, op.ID))
	var v any
	switch vendor {
	case VendorGemini:
		v = struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}{name, op.Description, op.Args}
	case VendorAnthropic:
		v = struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		}{name, op.Description, op.Args}
	case VendorOpenAI:
		type fn struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}
		v = struct {
			Type     string `json:"type"`
			Function fn     `json:"function"`
		}{"function", fn{name, op.Description, op.Args}}
	default:
		return nil, fmt.Errorf("there is no vendor %q: use gemini, anthropic or openai", vendor)
	}
	return json.Marshal(v)
}

// errorSchema is a typed error's schema in an OpenAPI document.
const errorSchema = `{"type":"object","description":"A typed connector error","properties":{
  "code":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"What went wrong"},
  "message":{"type":"string","description":"Why, for a person"},
  "retryable":{"type":"boolean","description":"Whether trying again later may succeed"},
  "retryAfter":{"type":"integer","description":"How long to wait before retrying, in milliseconds"}},"required":["code","message","retryable"]}`

// HookHeader is the request header an OpenAPI callback's URL expression
// reads: where the service should push the callback.
const HookHeader = "Cx-Hook-Url"

// OpenAPI is a contract as an OpenAPI 3.1 document: each operation a POST
// path /<operation> (its args the request body, its result the 200, a
// typed error otherwise), its declared callbacks as the operation's
// callbacks, and the contract's events as top-level webhooks. x-
// extensions keep what OpenAPI has no word for (effect, destructive, key,
// delivery, subscription, id fields, correlation), so 15e reads the
// document back into the same contract.
func OpenAPI(c Contract) json.RawMessage {
	content := func(schema json.RawMessage) map[string]any {
		return map[string]any{"application/json": map[string]any{"schema": schema}}
	}
	events := map[string]EventDef{}
	for _, e := range c.Events {
		events[e.ID] = e
	}
	webhookOp := func(e EventDef) map[string]any {
		op := map[string]any{"operationId": WireName(c.ID + ".event." + e.ID), "summary": e.Title, "description": e.Description,
			"requestBody": map[string]any{"required": true, "content": content(e.Payload)},
			"responses":   map[string]any{"200": map[string]any{"description": "Received"}},
			"x-delivery":  e.Delivery, "x-id-fields": e.IDFields}
		if e.Subscription != "" {
			op["x-subscription"] = e.Subscription
		}
		return map[string]any{"post": op}
	}
	paths := map[string]any{}
	for _, op := range c.Operations {
		o := map[string]any{"operationId": WireName(ToolName(c.ID, op.ID)), "summary": op.Title, "description": op.Description,
			"requestBody": map[string]any{"required": true, "content": content(op.Args)},
			"responses": map[string]any{
				"200":     map[string]any{"description": "The result", "content": content(op.Result)},
				"default": map[string]any{"description": "A typed error", "content": content(json.RawMessage(`{"$ref":"#/components/schemas/Error"}`))},
			},
			"x-effect": op.Effect}
		if op.Destructive {
			o["x-destructive"] = true
		}
		if op.Key != "" {
			o["x-idempotency-key"] = op.Key
		}
		if len(op.Errors) > 0 {
			o["x-errors"] = op.Errors
		}
		if len(op.Callbacks) > 0 {
			cbs := map[string]any{}
			for _, cb := range op.Callbacks {
				e := events[cb.Event]
				item := webhookOp(e)
				item["post"].(map[string]any)["x-correlate"] = map[string]any{"result": cb.Correlate.Result, "event": cb.Correlate.Event}
				cbs[cb.Event] = map[string]any{"{$request.header." + HookHeader + "}": item}
			}
			o["callbacks"] = cbs
		}
		paths["/"+op.ID] = map[string]any{"post": o}
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": c.Meta.Title, "description": c.Meta.Description, "version": fmt.Sprint(c.Version),
			"x-contract": c.ID, "x-tags": c.Meta.Tags},
		"paths":      paths,
		"components": map[string]any{"schemas": map[string]any{"Error": json.RawMessage(errorSchema)}},
	}
	if len(c.Events) > 0 {
		hooks := map[string]any{}
		for _, e := range c.Events {
			hooks[e.ID] = webhookOp(e)
		}
		doc["webhooks"] = hooks
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	return raw
}
