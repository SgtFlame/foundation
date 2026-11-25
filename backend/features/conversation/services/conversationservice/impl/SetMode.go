package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
)

// SetMode stores the mode one of the principal's conversations runs its
// next rounds in (forge 15e). Setting the mode it has already is a no-op,
// so the switch is idempotent.
func (s *service) SetMode(args conversationservice.SetModeArgs) conversationservice.SetModeResponse {
	if failed := check(args.ServiceArgs, "conversation.write", args.ConversationID); failed != nil {
		return conversationservice.SetModeResponse{Common: *failed}
	}
	if s.config == nil {
		return conversationservice.SetModeResponse{Common: *args.Fail(http.StatusServiceUnavailable, log.Code("chat_not_configured").New("chat is not configured"))}
	}
	modes, err := conversationsettings.ReadModes(s.config, args.ServiceArgs)
	if err != nil {
		return conversationservice.SetModeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	mode, ok := modes.Find(args.Mode)
	if !ok || args.Mode == "" {
		return conversationservice.SetModeResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("unknown_mode").With("mode", args.Mode).Errorf("there is no chat mode %q", args.Mode))}
	}
	c, failed := s.find(args.ServiceArgs, args.ConversationID)
	if failed != nil {
		return conversationservice.SetModeResponse{Common: *failed}
	}
	if c.Mode != mode.ID {
		if err := s.owned(args.ServiceArgs).Model(&c).Update("mode", mode.ID).Error; err != nil {
			return conversationservice.SetModeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
		c.Mode = mode.ID
	}
	return conversationservice.SetModeResponse{Common: features.OK(), Conversation: c}
}
