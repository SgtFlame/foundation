package conversationsettings

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
)

// MadeModes gives the modes people made that the principal sees (forge
// 15h, features/conversation/made): their own, and those shared with the
// tenant. They are records of the live core, not settings, so a person's
// modes never freeze the settings' list at the user scope.
type MadeModes interface {
	Visible(args features.ServiceArgs) ([]Mode, error)
}

var made MadeModes

// SetMadeModes sets where ReadModes finds the modes people made: serve.go
// sets it once, before serving (as authz.SetAgents is set). Without it only
// the settings' modes exist.
func SetMadeModes(m MadeModes) { made = m }

// ReadModes is the modes the principal sees: the chat.modes settings, then
// the modes people made whose ids the settings do not already use. Every
// reader of the chat modes goes through it.
func ReadModes(r config.Reader, args features.ServiceArgs) (Modes, error) {
	modes, err := config.Read[Modes](r, args, ModesID)
	if err != nil || made == nil {
		return modes, err
	}
	extra, err := made.Visible(args)
	if err != nil {
		return modes, err
	}
	for _, m := range extra {
		if _, taken := modes.Find(m.ID); !taken {
			modes.Modes = append(modes.Modes, m)
		}
	}
	return modes, nil
}
