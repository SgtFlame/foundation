package secrets

import "github.com/SgtFlame/foundation/backend/features/db"

// Audit actions.
const (
	AuditPut    = "put"
	AuditDelete = "delete"
)

// SecretAudit records every change made through the Secrets editor: who,
// which secret and version, and when. Never the value.
type SecretAudit struct {
	db.Model
	Name    string `gorm:"index;not null"` // "server/gemini-api-key", "config/app/ai.providers/gemini.apiKey"
	Action  string `gorm:"not null"`
	Version int
	UserID  uint64 `gorm:"index"`
}

// Models are the secrets feature's tables, for the application database.
func Models() []any {
	return []any{&SecretAudit{}}
}
