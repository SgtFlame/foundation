package impl

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db       *gorm.DB
	store    chronicle.Store
	embedder embedservice.Service
	userName func(user uint64) string

	indexMu  sync.Mutex     // one entry is indexed at a time, so the latest text wins
	indexing sync.WaitGroup // indexing in the background, for tests
}

var log = logging.In("chronicle")

const (
	maxTitle   = 200
	maxShort   = 2 << 10
	maxSummary = 32 << 10
	maxContext = 256 << 10
)

// resource is the authz path of an entry; id 0 is the principal's entries
// as a whole.
func resource(p features.Principal, id uint64) string {
	if id == 0 {
		return authz.Tenant(p) + "/" + authz.Resource("user", p.UserID) + "/chronicles"
	}
	return authz.Tenant(p) + "/" + authz.Resource("chronicle", id)
}

// check runs authz.Check for action on the entry.
func check(args features.ServiceArgs, action string, id uint64) *features.ServiceResponse {
	res := resource(args.Principal, id)
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// owned is the principal's own rows of a chronicle table.
func (s *service) owned(args features.ServiceArgs) *gorm.DB {
	return chronicle.Owned(s.db.WithContext(args.Context()), args.Principal)
}

// find loads one of the principal's entries; another user's is not found.
func (s *service) find(args features.ServiceArgs, id uint64) (chronicle.Entry, *features.ServiceResponse) {
	var e chronicle.Entry
	err := s.owned(args).First(&e, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return e, args.Fail(http.StatusNotFound, log.Code("entry_not_found").With("entryId", id).New("there is no such entry"))
	}
	if err != nil {
		return e, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return e, nil
}

// findVisible loads an entry the principal may read: theirs, or one shared
// with them.
func (s *service) findVisible(args features.ServiceArgs, id uint64) (chronicle.Entry, *features.ServiceResponse) {
	var e chronicle.Entry
	err := chronicle.Visible(s.db.WithContext(args.Context()), args.Principal).Where("status = ? OR owner_id = ?", chronicle.StatusActive, args.Principal.UserID).First(&e, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return e, args.Fail(http.StatusNotFound, log.Code("entry_not_found").With("entryId", id).New("there is no such entry"))
	}
	if err != nil {
		return e, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return e, nil
}

// mine tells whether e is the principal's own entry.
func mine(p features.Principal, e chronicle.Entry) bool {
	return e.TenantID == p.TenantID && e.OwnerID == p.UserID
}

// apply validates in and copies it onto e, whose ID is 0 for a new entry.
func (s *service) apply(args features.ServiceArgs, in chronicleservice.EntryInput, e *chronicle.Entry) *features.ServiceResponse {
	bad := func(code, format string, a ...any) *features.ServiceResponse {
		return args.Fail(http.StatusBadRequest, log.Code(code).Errorf(format, a...))
	}
	t, ok := chronicle.TypeOf(in.Type)
	if !ok {
		return bad("unknown_type", "there is no entry type %q", in.Type)
	}
	title := strings.TrimSpace(in.Title)
	switch {
	case title == "":
		return bad("empty_title", "the entry needs a title")
	case utf8.RuneCountInString(title) > maxTitle:
		return bad("title_too_long", "the title is longer than %d characters", maxTitle)
	case len(in.ShortSummary) > maxShort:
		return bad("summary_too_long", "the short summary is longer than %d KB", maxShort>>10)
	case len(in.FullSummary) > maxSummary:
		return bad("summary_too_long", "the full summary is longer than %d KB", maxSummary>>10)
	case len(in.FullContext) > maxContext:
		return bad("context_too_long", "the full context is longer than %d KB", maxContext>>10)
	}

	fields := in.Fields
	if len(fields) == 0 || string(fields) == "null" {
		fields = json.RawMessage(`{}`)
	}
	var value map[string]any
	if err := json.Unmarshal(fields, &value); err != nil {
		return bad("invalid_fields", "the fields are not a JSON object")
	}
	if schema, err := config.ParseSchema(t.Fields); err == nil {
		// Fields of another type, kept from before a type change, are dropped
		for k := range value {
			if _, ok := schema.Properties[k]; !ok {
				delete(value, k)
			}
		}
		if errs := schema.Validate(value); len(errs) > 0 {
			return bad("invalid_fields", "%s: %s", errs[0].Path, errs[0].Message)
		}
		fields, _ = json.Marshal(value)
	}

	if in.ParentID != 0 {
		if in.ParentID == e.ID {
			return bad("invalid_parent", "an entry cannot be its own parent")
		}
		var entries []chronicle.Entry
		if err := s.owned(args).Select("id", "parent_id").Find(&entries).Error; err != nil {
			return args.Fail(http.StatusInternalServerError, log.Wrap(err))
		}
		byID := make(map[uint64]chronicle.Entry, len(entries))
		for _, x := range entries {
			byID[x.ID] = x
		}
		if _, ok := byID[in.ParentID]; !ok {
			return bad("invalid_parent", "there is no such parent entry")
		}
		for _, ancestor := range chronicle.WithAncestors([]uint64{in.ParentID}, byID) {
			if e.ID != 0 && ancestor == e.ID {
				return bad("invalid_parent", "an entry cannot be moved under its own child")
			}
		}
	}

	e.Type, e.Title, e.ParentID, e.Fields = t.ID, title, in.ParentID, string(fields)
	e.ShortSummary, e.FullSummary, e.FullContext = strings.TrimSpace(in.ShortSummary), strings.TrimSpace(in.FullSummary), in.FullContext
	return nil
}
