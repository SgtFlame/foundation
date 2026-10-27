// Package secretsservice maintains the server's secrets for Application
// Administrators (documentation/Secrets.md › secretsservice). It is
// write-only: no operation ever returns a secret's value.
package secretsservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets"
)

type Service interface {
	List(args ListArgs) ListResponse
	Put(args PutArgs) PutResponse
	Delete(args DeleteArgs) DeleteResponse
	Status(args StatusArgs) StatusResponse
}

// Kinds of secret.
const (
	KindServer  = "server"  // server/<name>: the bootstrap bundle in Vault, read when the server unseals
	KindSetting = "setting" // config/app/<section>/<path>: a secret setting at the app scope
)

// Secret describes one secret, never its value.
type Secret struct {
	Name      string // its path in Vault: "server/gemini-api-key", "config/app/ai.providers/gemini.apiKey"
	Kind      string
	Title     string // a setting's section and field titles
	Set       bool
	Version   int // in Vault; 0 without it
	UpdatedAt time.Time
	UpdatedBy string // a user's name, or "<name> (operator)" from the scripts
	// Source is where a setting's value comes from when it is not stored at
	// the app scope: "env" (a flag or env var, which wins) or "server"
	Source string
	// Protected secrets cannot be changed here: the database key is
	// rotated with rotate-db-key.sh
	Protected bool
}

// ListArgs lists the secrets whose names start with Prefix.
type ListArgs struct {
	features.ServiceArgs
	Prefix string
}

type ListResponse struct {
	Common  features.ServiceResponse
	Secrets []Secret
}

// PutArgs writes a new version of the secret Name: server/<name>, a new one
// allowed, or a listed setting.
type PutArgs struct {
	features.ServiceArgs
	Name  string
	Value string
}

type PutResponse struct {
	Common features.ServiceResponse
	Secret Secret
}

// DeleteArgs soft-deletes a server secret (its versions are kept) or
// clears a setting.
type DeleteArgs struct {
	features.ServiceArgs
	Name string
}

type DeleteResponse struct {
	Common features.ServiceResponse
}

type StatusArgs struct {
	features.ServiceArgs
}

type StatusResponse struct {
	Common features.ServiceResponse
	Status secrets.Status
}
