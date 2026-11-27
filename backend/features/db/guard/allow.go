package guard

const (
	gormImp = "gorm.io/gorm"
	jsImp   = "github.com/nats-io/nats.go/jetstream"
)

// Allow is Foundation's allow list (documentation/DataServices.md). Rows
// with an Until are removed by that sub-part; the list only shrinks.
var Allow = []Row{
	// Wiring and infrastructure: they open the stores and hand them to
	// their owners, or run NATS itself.
	{Package: "cli/cmd", Owner: "serve (wiring)", Storage: "opens every store"},
	{Package: "features/db", Owner: "db", Storage: "the databases' handle provider", Imports: []string{gormImp}},
	{Package: "features/live/natsd", Owner: "natsd", Storage: "NATS itself", Imports: []string{jsImp}},

	// The application database (21d-1).
	{Package: "features/identity", Owner: "identity", Storage: "application DB: users, tenants, memberships, refresh tokens, signing keys", Imports: []string{gormImp}},
	{Package: "features/identity/services/identityservice/impl", Owner: "identity", Storage: "application DB", Imports: []string{gormImp}},
	{Package: "features/identity/services/tenantservice/impl", Owner: "identity", Storage: "application DB", Imports: []string{gormImp}},
	{Package: "features/identity/services/userservice/impl", Owner: "identity", Storage: "application DB", Imports: []string{gormImp}},
	{Package: "features/authz", Owner: "authz", Storage: "application DB: grants, custom roles, access audit", Imports: []string{gormImp}},
	{Package: "features/authz/services/authzservice/impl", Owner: "authz", Storage: "application DB", Imports: []string{gormImp}},
	{Package: "features/secrets", Owner: "secrets", Storage: "application DB: secret audit", Imports: []string{gormImp}},
	{Package: "features/secrets/services/secretsservice/impl", Owner: "secrets", Storage: "application DB", Imports: []string{gormImp}},
	{Package: "features/logs/services/logsservice/impl", Owner: "logs", Storage: "application DB: user log levels", Imports: []string{gormImp}},

	// Runtime tables of the tenant database (21d-2).
	{Package: "features/config/services/configservice/impl", Owner: "config", Storage: "tenant DB: settings", Imports: []string{gormImp}},
	{Package: "features/usage", Owner: "usage", Storage: "tenant DB: usage records and captures", Imports: []string{gormImp}},
	{Package: "features/usage/services/usageservice/impl", Owner: "usage", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/conversation", Owner: "conversation", Storage: "tenant DB: conversations, rounds, segments, feedback", Imports: []string{gormImp}},
	{Package: "features/conversation/services/conversationservice/impl", Owner: "conversation", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/chronicle", Owner: "chronicle", Storage: "tenant DB: Chronicle entries, links, rounds, shares", Imports: []string{gormImp}},
	{Package: "features/chronicle/services/chronicleservice/impl", Owner: "chronicle", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/retrieval", Owner: "retrieval", Storage: "tenant DB: segment feedback and votes", Imports: []string{gormImp}},
	{Package: "features/retrieval/services/retrievalservice/impl", Owner: "retrieval", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/retrieval/retrievaltest", Owner: "retrieval", Storage: "test fixture", Imports: []string{gormImp}},
	{Package: "features/cognition/traces", Owner: "cognition", Storage: "tenant DB: Cognition runs and steps", Imports: []string{gormImp}},
	{Package: "features/cognition/services/cognitionservice/impl", Owner: "cognition", Storage: "tenant DB", Imports: []string{gormImp}},

	// Definition copies and large tables (21d-3): the followers that write
	// them are the owner's, named per database instance (db.Named).
	{Package: "features/records/entities", Owner: "entity", Storage: "tenant DB: entity versions", Imports: []string{gormImp}},
	{Package: "features/records/services/entityservice/impl", Owner: "entity", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/ui/prefabs", Owner: "prefab", Storage: "tenant DB: prefab copies", Imports: []string{gormImp}},
	{Package: "features/ui/services/prefabservice/impl", Owner: "prefab", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/apps/manifests", Owner: "app", Storage: "tenant DB: app rows, versions", Imports: []string{gormImp}},
	{Package: "features/apps/installs", Owner: "app", Storage: "tenant DB: installs", Imports: []string{gormImp}},
	{Package: "features/apps/services/appservice/impl", Owner: "app", Storage: "tenant DB", Imports: []string{gormImp}},
	{Package: "features/records/data", Owner: "record", Storage: "tenant DB: large tables rec_*; record projections"},

	// The live core's streams and buckets (21d-4): held by the data and
	// runner processes; everyone else calls svc.live and svc.files.
	{Package: "features/live/project", Owner: "live", Storage: "tenant streams, projections, side buckets", Imports: []string{jsImp}},
	{Package: "features/live/bus", Owner: "live", Storage: "tenant streams", Imports: []string{jsImp}},
	{Package: "features/live/presence", Owner: "live", Storage: "presence buckets (the live core's, set through svc.live elsewhere later)", Imports: []string{jsImp}},
	{Package: "features/live/nodes", Owner: "live", Storage: "trigger events' streams, read by the runners' trigger consumers", Imports: []string{jsImp}},
	{Package: "features/activity", Owner: "live", Storage: "activity streams, written by the live core's followers", Imports: []string{jsImp}},
	{Package: "features/ui", Owner: "ui (web)", Storage: "UI session buckets, held by the process holding the browser's socket (21d-4 question 2)", Imports: []string{jsImp}},
	{Package: "features/records/files", Owner: "files", Storage: "record files' object stores (filesservice's store)", Imports: []string{jsImp}},
	{Package: "features/apps/branches", Owner: "live", Storage: "branch histories read from the def streams", Imports: []string{jsImp}},
	{Package: "features/jobs", Owner: "jobs", Storage: "job work queues and schedules", Imports: []string{jsImp}},

	// Connectors (21e-1): the adapter runtime and scripted connectors keep
	// their books through connectors.Books, held by these alone.
	{Package: "features/connectors", Owner: "connectors", Storage: "connector KV (cx_*)", Imports: []string{jsImp}},
	{Package: "features/connectors/services/connectorsservice/impl", Owner: "connectors", Storage: "connector KV and CX streams, for adapters without a handle", Imports: []string{jsImp}},
	{Package: "features/connectors/connections", Owner: "connectors", Storage: "connector KV", Imports: []string{jsImp}},

	// The Control Plane's own account and its database rows (iteration 20):
	// not tenant storage.
	{Package: "features/controlplane", Owner: "controlplane", Storage: "control account and desired state"},
	{Package: "features/controlplane/agent", Owner: "controlplane", Storage: "control account", Imports: []string{jsImp}},
	{Package: "features/controlplane/ctl", Owner: "controlplane", Storage: "control account", Imports: []string{jsImp}},
	{Package: "features/controlplane/daemon", Owner: "controlplane", Storage: "control account", Imports: []string{jsImp}},
	{Package: "features/controlplane/issuer", Owner: "controlplane", Storage: "control account", Imports: []string{jsImp}},
	{Package: "features/controlplane/packages", Owner: "controlplane", Storage: "control account object store", Imports: []string{jsImp}},
}
