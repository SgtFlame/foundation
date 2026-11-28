// Package images is AI image generation (forge 35f-7, documentation
// Connectors.md › Built in forge 35f-7): the provider-neutral contract
// image.generate@1, the connector type image.google over the Gemini API's
// image models with chat's key selection, the Generate image node and the
// image.generate tool. Every call goes through the connector bus.
package images

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The contract and the Google type.
const (
	GenerateID  = "image.generate"
	GenerateRef = "image.generate@1"
	TypeID      = "image.google"
	source      = "images"
)

// AspectRatios are the ratios generate takes.
var AspectRatios = []string{"1:1", "3:4", "4:3", "9:16", "16:9", "2:3", "3:2", "21:9"}

// MaxCount is the most images one generate call makes.
const MaxCount = 4

const fileSchema = `{"type":"object","description":"A file in the tenant's file store, as a file field holds it","properties":{
  "id":{"type":"string","description":"The file's id"},
  "name":{"type":"string","description":"Its name"},
  "size":{"type":"integer","description":"Its size in bytes"},
  "contentType":{"type":"string","description":"Its media type, e.g. image/png"}},"required":["id"]}`

// Contract is image.generate@1.
func Contract() connectors.Contract {
	ratios, _ := json.Marshal(AspectRatios)
	return connectors.Contract{
		ID: GenerateID, Version: 1,
		Meta: connectors.Meta{Title: "Image generation", Description: "Makes images from a text prompt with an AI image model, optionally matching or editing reference images, and keeps them as files in the tenant's file store; for illustrating a page, sketching a character or drawing a map.", Tags: []string{"image", "ai", "generation", "illustration"}},
		Operations: []connectors.Operation{
			{
				ID: "generate", Title: "Generate images", Description: "Makes up to four images of the prompt and stores each as a file the caller uploaded, returning the files with the prompt and the model. A refusal on the model's safety policy is the refused error, shown as such; it costs money, so a chat asks first.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "prompt":{"type":"string","description":"What the image shows, in words: subject, setting, composition, mood"},
  "aspectRatio":{"type":"string","enum":` + string(ratios) + `,"description":"Width to height; 1:1 when left out"},
  "count":{"type":"integer","minimum":1,"maximum":4,"description":"How many variants to make; 1 when left out"},
  "style":{"type":"string","description":"A style to draw in, e.g. watercolour, technical diagram, ink sketch"},
  "reference":{"type":"array","maxItems":3,"description":"Images to match or edit, files the caller uploaded or may read","items":` + fileSchema + `}},"required":["prompt"]}`),
				Result: json.RawMessage(`{"type":"object","properties":{
  "images":{"type":"array","description":"The images made, each a stored file","items":{"type":"object","properties":{
    "file":` + fileSchema + `,
    "prompt":{"type":"string","description":"The prompt it was made from, with the style"},
    "model":{"type":"string","description":"The model that made it"}},"required":["file","prompt","model"]}},
  "model":{"type":"string","description":"The model used"},
  "text":{"type":"string","description":"What the model said beside the images, if anything"}},"required":["images","model"]}`),
				Examples: []connectors.Example{{
					Args:   json.RawMessage(`{"prompt":"A lighthouse on a cliff at dusk","aspectRatio":"16:9","style":"watercolour"}`),
					Result: json.RawMessage(`{"images":[{"file":{"id":"f-1","name":"image-1.png","size":204800,"contentType":"image/png"},"prompt":"A lighthouse on a cliff at dusk, in watercolour","model":"gemini-2.5-flash-image"}],"model":"gemini-2.5-flash-image"}`),
				}},
				Errors: []string{connectors.CodeInvalid, connectors.CodeRateLimited, connectors.CodeRefused, connectors.CodeAuth, connectors.CodeUnavailable},
			},
			{
				ID: "check", Title: "Check image generation", Description: "Reports whether image generation is available to the caller: a model and an API key the caller's settings give.",
				Effect:   connectors.EffectRead,
				Args:     json.RawMessage(`{"type":"object","properties":{}}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true; otherwise an error"},"model":{"type":"string","description":"The model generate uses"}},"required":["ok"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true,"model":"gemini-2.5-flash-image"}`)}},
				Errors:   []string{connectors.CodeUnavailable},
			},
		},
	}
}

// Type is image.google, which needs no connection: it uses the AI
// provider's key as the caller's settings give it, as chat does.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{GenerateRef}, Source: connectors.SourceBuiltin, NoConnection: true,
		Meta: connectors.Meta{Title: "Google image generation", Description: "Generates images with Gemini's native image model on the Gemini API, using the same API key as chat (the AI provider settings as the caller sees them), and records each call's usage and cost.", Tags: []string{"image", "ai", "google", "gemini"}},
	}
}

// Contribute registers the contract and the type.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, source, []connectors.Contract{Contract()}, []connectors.Type{Type()})
}
