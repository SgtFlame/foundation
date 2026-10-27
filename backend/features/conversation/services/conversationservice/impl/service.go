package impl

import (
	"errors"
	"net/http"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db       *gorm.DB
	chat     chatservice.Service
	config   config.Reader
	registry *extensions.Registry

	mu      sync.Mutex
	waiting map[string]*waiting // tool calls rounds are paused on, by waitKey
}

var log = logging.In("conversation")

// resource is the authz path of a conversation; id 0 is the principal's
// conversations as a whole.
func resource(p features.Principal, id uint64) string {
	if id == 0 {
		return authz.Tenant(p) + "/" + authz.Resource("user", p.UserID) + "/conversations"
	}
	return authz.Tenant(p) + "/" + authz.Resource("conversation", id)
}

// check runs authz.Check for action on the conversation.
func check(args features.ServiceArgs, action string, id uint64) *features.ServiceResponse {
	res := resource(args.Principal, id)
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// owned is the principal's own conversations.
func (s *service) owned(args features.ServiceArgs) *gorm.DB {
	return s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).Where("owner_id = ?", args.Principal.UserID)
}

// find loads one of the principal's conversations; another user's is not found.
func (s *service) find(args features.ServiceArgs, id uint64) (conversation.Conversation, *features.ServiceResponse) {
	var c conversation.Conversation
	err := s.owned(args).First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, args.Fail(http.StatusNotFound, log.Code("conversation_not_found").With("conversationId", id).New("there is no such conversation"))
	}
	if err != nil {
		return c, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return c, nil
}
