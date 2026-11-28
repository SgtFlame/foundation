package images

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// NodeID and ToolID are the Generate image node's and tool's ids.
const (
	NodeID = "ai.generateImage"
	ToolID = "image.generate"
)

// Generate calls image.generate@1 generate through the connector bus as
// the call's principal.
func Generate(args features.ServiceArgs, d connectors.Deps, a GenerateArgs) (GenerateResult, *connectors.Error) {
	if d.Dispatcher == nil {
		return GenerateResult{}, connectors.Errorf(connectors.CodeUnavailable, "there is no connector bus here")
	}
	raw, _ := json.Marshal(a)
	out, e := d.Dispatcher.Call(args, connectors.Call{Contract: GenerateRef, Operation: "generate", Args: raw})
	if e != nil {
		return GenerateResult{}, e
	}
	var r GenerateResult
	if err := json.Unmarshal(out, &r); err != nil {
		return GenerateResult{}, connectors.Errorf(connectors.CodeUnavailable, "the reply is not generate's result: %v", err)
	}
	return r, nil
}

var sample = cognition.Values{
	"images": []any{map[string]any{"file": map[string]any{"id": "f-1", "name": "image-1.png", "size": 204800, "contentType": "image/png"}, "prompt": "A lighthouse on a cliff at dusk", "model": "gemini-2.5-flash-image"}},
	"files":  []any{map[string]any{"id": "f-1", "name": "image-1.png", "size": 204800, "contentType": "image/png"}},
	"model":  "gemini-2.5-flash-image",
}

// ContributeNodes registers the Generate image node and the
// image.generate tool, both calling image.generate@1 through the bus.
func ContributeNodes(reg *extensions.Registry, deps func() connectors.Deps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeID, Source: source, Order: 960}, Title: "Generate image", Category: "LLM",
		Effects: true, External: true, Model: true, Sample: sample,
		Description: "Makes images from a prompt with an AI image model (image.generate@1 through the connector bus, Google's by default, with the AI key the run's principal has) and stores them as files the principal uploaded, ready to set on a file field or insert in a page. A safety refusal, a busy model or a bad prompt leaves by Error with its code (refused, rateLimited, invalid). It costs money per image.",
		Example:     "Generate image of the selected paragraph's scene, 16:9 in watercolour, then Set field cover on the page record with its first file",
		Examples:    cognition.ConfigExample(NodeID, `{"aspectRatio":"16:9","count":1}`),
		Tags:        []string{"image", "ai", "illustration", "generation"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"aspectRatio":{"type":"string","title":"Aspect ratio","description":"Width to height, one of 1:1, 3:4, 4:3, 9:16, 16:9, 2:3, 3:2, 21:9; 1:1 when empty"},
			"count":{"type":"integer","title":"Count","description":"How many variants, 1 to 4; 1 when empty"},
			"style":{"type":"string","title":"Style","description":"A style when the Style input is unconnected, e.g. ink sketch"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("prompt", "Prompt", "string", "What the image shows"),
				{ID: "style", Name: "Style", Type: "string", Optional: true, Description: "A style to draw in; the config's when unconnected"},
				{ID: "reference", Name: "Reference", Type: "json", Optional: true, Description: "Images to match or edit: [{id}] of files the principal uploaded or may read"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Done"), cognition.ExecPin("error", "Error"),
				cognition.DataPin("images", "Images", "json", "[{file: {id, name, size, contentType}, prompt, model}]"),
				cognition.DataPin("files", "Files", "json", "The images' files, [{id, name, size, contentType}], for a file field"),
				cognition.DataPin("model", "Model", "string", "The model that made them"),
				cognition.DataPin("failure", "Failure", "json", "Why it failed: {code, message, retryable}")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				AspectRatio string
				Count       int
				Style       string
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			a := GenerateArgs{AspectRatio: c.AspectRatio, Count: c.Count, Style: c.Style}
			a.Prompt, _ = cognition.Get[string](in, "prompt")
			if s, _ := cognition.Get[string](in, "style"); s != "" {
				a.Style = s
			}
			if ref := in["reference"]; ref != nil {
				b, _ := json.Marshal(ref)
				if err := json.Unmarshal(b, &a.Reference); err != nil {
					return nil, "", fmt.Errorf("reference is not a list of files: %v", err)
				}
			}
			r, e := Generate(x.ServiceArgs, deps(), a)
			if e != nil {
				return cognition.Values{"failure": map[string]any{"code": e.Code, "message": e.Message, "retryable": e.Retry}}, "error", nil
			}
			files := make([]any, len(r.Images))
			images := make([]any, len(r.Images))
			for i, img := range r.Images {
				files[i] = img.File
				images[i] = img
			}
			return cognition.Values{"images": images, "files": files, "model": r.Model}, cognition.Then, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: ToolID, Source: source}, Title: "Generate image", Effect: tools.EffectWrite,
		Description: "Makes up to four images from a prompt with an AI image model and stores them as files the user uploaded, returning their ids, names and the model. Use it when the user asks for an illustration, a sketch or a picture; describe the subject, setting and composition in the prompt. It costs money, so the user approves each call. A safety refusal comes back as an error to tell the user.",
		Input: json.RawMessage(`{"type":"object","properties":{
  "prompt":{"type":"string","description":"What the image shows: subject, setting, composition, mood"},
  "aspectRatio":{"type":"string","enum":["1:1","3:4","4:3","9:16","16:9","2:3","3:2","21:9"],"description":"Width to height; 1:1 when left out"},
  "count":{"type":"integer","minimum":1,"maximum":4,"description":"How many variants; 1 when left out"},
  "style":{"type":"string","description":"A style, e.g. watercolour, technical diagram"}},"required":["prompt"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[GenerateArgs](call)
			if err != nil {
				return tools.Result{}, err
			}
			r, e := Generate(call.ServiceArgs, deps(), a)
			if e != nil {
				return tools.Result{}, fmt.Errorf("%s: %s", e.Code, e.Message)
			}
			images := make([]any, len(r.Images))
			for i, img := range r.Images {
				images[i] = map[string]any{"file": img.File, "prompt": img.Prompt}
			}
			summary := fmt.Sprintf("Made %d image(s) with %s", len(r.Images), r.Model)
			return tools.Result{Content: map[string]any{"images": images, "model": r.Model}, Summary: summary}, nil
		},
	})
}
