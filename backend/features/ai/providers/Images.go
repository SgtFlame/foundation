package providers

import "context"

// Images is a provider of image generation (forge 35f-7): the gemini
// provider's native image models, wrapped by usage.ImageProvider so every
// call is recorded and priced like a chat call.
type Images interface {
	GenerateImages(ctx context.Context, in ImageInput) (ImageOutput, error)
}

// ImageInput asks for Count images of Prompt, at AspectRatio ("1:1",
// "16:9", …; the model's default when empty), optionally matching or
// editing the References.
type ImageInput struct {
	Model       string // the provider's model id; its default when empty
	Prompt      string
	AspectRatio string
	Count       int // 1 when 0
	References  []Image
}

// Image is one image's bytes and media type.
type Image struct {
	Data        []byte
	ContentType string
}

// ImageOutput is the images made, the model's words beside them, and the
// tokens the calls used. Refused is set, with Reason, when the model
// declined on its safety policy and made no image.
type ImageOutput struct {
	Model   string
	Images  []Image
	Text    string
	Refused bool
	Reason  string
	Usage   ImageUsage
}

// ImageUsage is the tokens image calls used; an image bills as output.
type ImageUsage struct {
	InputTokens, OutputTokens int
}
