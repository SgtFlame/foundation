package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/ai/images"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// imagesEnv is the env the images feature was contributed with, read by
// its in-process type when a call comes.
var imagesEnv *Env

// Images (forge 35f-7): image.generate@1, the image.google type over the
// Gemini API with chat's key selection, the Generate image node and the
// image.generate tool.
func init() {
	Register(Feature{
		Name: "images",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			imagesEnv = env
			images.Contribute(reg)
			images.ContributeNodes(reg, func() connectors.Deps { return env.Connectors() })
			return nil
		},
		Adapter: func(o *adapter.Options) {
			o.Types = append(o.Types, images.Type())
			o.Contracts = append(o.Contracts, images.Contract())
			o.Impls = append(o.Impls, images.Impl(func() images.Deps {
				return images.Deps{Images: imagesEnv.ImageProvider(), Files: imagesEnv.RecordFiles()}
			}))
		},
	})
}
