package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
)

// Modes lists the chat.modes settings' modes with their graphs' spec.
func (s *service) Modes(args conversationservice.ModesArgs) conversationservice.ModesResponse {
	if failed := check(args.ServiceArgs, "conversation.read", 0); failed != nil {
		return conversationservice.ModesResponse{Common: *failed}
	}
	if s.config == nil {
		return conversationservice.ModesResponse{Common: *args.Fail(http.StatusServiceUnavailable, log.Code("chat_not_configured").New("chat is not configured"))}
	}
	modes, err := config.Read[conversationsettings.Modes](s.config, args.ServiceArgs, conversationsettings.ModesID)
	if err != nil {
		return conversationservice.ModesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	resp := conversationservice.ModesResponse{Common: features.OK(), Modes: []conversationservice.ModeInfo{}}
	def, _ := modes.Default()
	for _, m := range modes.Modes {
		info := conversationservice.ModeInfo{ID: m.ID, Label: m.Label, Icon: m.Icon, GraphRef: m.GraphRef, Default: m.ID == def.ID, Focus: m.Focus, Missing: true}
		if s.engine != nil {
			if g, ok := s.engine.Graph(args.ServiceArgs, m.GraphRef); ok {
				info.Missing, info.Description, info.Settings = false, g.Description, cognition.ModeSpecOf(g).Settings
			}
		}
		resp.Modes = append(resp.Modes, info)
	}
	return resp
}
