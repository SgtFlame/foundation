/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers/gemini"
	"github.com/SgtFlame/foundation/backend/features/ai/rest/chathandlers"
	"github.com/SgtFlame/foundation/backend/features/ai/rest/embedhandlers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	chatimpl "github.com/SgtFlame/foundation/backend/features/ai/services/chatservice/impl"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	embedimpl "github.com/SgtFlame/foundation/backend/features/ai/services/embedservice/impl"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	apphandlers "github.com/SgtFlame/foundation/backend/features/apps/rest/apphandlers"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	appimpl "github.com/SgtFlame/foundation/backend/features/apps/services/appservice/impl"
	apptools "github.com/SgtFlame/foundation/backend/features/apps/tools"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/rest/authzhandlers"
	authzimpl "github.com/SgtFlame/foundation/backend/features/authz/services/authzservice/impl"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	chroniclecontext "github.com/SgtFlame/foundation/backend/features/chronicle/context"
	chroniclenodes "github.com/SgtFlame/foundation/backend/features/chronicle/nodes"
	"github.com/SgtFlame/foundation/backend/features/chronicle/rest/chroniclehandlers"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	chronicleimpl "github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice/impl"
	chroniclesource "github.com/SgtFlame/foundation/backend/features/chronicle/source"
	chroniclesteps "github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/chronicle/store/entrystore"
	chronicletools "github.com/SgtFlame/foundation/backend/features/chronicle/tools"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/cognition/rest/cognitionhandlers"
	cognitionimpl "github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice/impl"
	cognitionsettings "github.com/SgtFlame/foundation/backend/features/cognition/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/rest/confighandlers"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	configimpl "github.com/SgtFlame/foundation/backend/features/config/services/configservice/impl"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	conversationnodes "github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/conversation/rest/conversationhandlers"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	conversationimpl "github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice/impl"
	conversationsettings "github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/extensions/rest/extensionshandlers"
	extensionsimpl "github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice/impl"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identityhandlers"
	"github.com/SgtFlame/foundation/backend/features/identity/rest/tenanthandlers"
	"github.com/SgtFlame/foundation/backend/features/identity/rest/userhandlers"
	identityimpl "github.com/SgtFlame/foundation/backend/features/identity/services/identityservice/impl"
	tenantimpl "github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice/impl"
	userimpl "github.com/SgtFlame/foundation/backend/features/identity/services/userservice/impl"
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexhandlers"
	indeximpl "github.com/SgtFlame/foundation/backend/features/index/services/indexservice/impl"
	indexsource "github.com/SgtFlame/foundation/backend/features/index/source"
	"github.com/SgtFlame/foundation/backend/features/index/store/lancestore"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/collabhandlers"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbhandlers"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/volumehandlers"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	collabimpl "github.com/SgtFlame/foundation/backend/features/kb/services/collabservice/impl"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	kbimpl "github.com/SgtFlame/foundation/backend/features/kb/services/kbservice/impl"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	volumeimpl "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice/impl"
	kbtools "github.com/SgtFlame/foundation/backend/features/kb/tools"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/rest/livehandlers"
	liveimpl "github.com/SgtFlame/foundation/backend/features/live/services/liveservice/impl"
	livesettings "github.com/SgtFlame/foundation/backend/features/live/settings"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/logs"
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logshandlers"
	logsimpl "github.com/SgtFlame/foundation/backend/features/logs/services/logsservice/impl"
	logssettings "github.com/SgtFlame/foundation/backend/features/logs/settings"
	"github.com/SgtFlame/foundation/backend/features/records"
	recordsdata "github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsfiles "github.com/SgtFlame/foundation/backend/features/records/files"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/rest/entityhandlers"
	"github.com/SgtFlame/foundation/backend/features/records/rest/recordhandlers"
	recordssearch "github.com/SgtFlame/foundation/backend/features/records/search"
	entityimpl "github.com/SgtFlame/foundation/backend/features/records/services/entityservice/impl"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	recordimpl "github.com/SgtFlame/foundation/backend/features/records/services/recordservice/impl"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
	recordstools "github.com/SgtFlame/foundation/backend/features/records/tools"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	retrievalcontext "github.com/SgtFlame/foundation/backend/features/retrieval/context"
	"github.com/SgtFlame/foundation/backend/features/retrieval/rest/retrievalhandlers"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	retrievalimpl "github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice/impl"
	retrievalsettings "github.com/SgtFlame/foundation/backend/features/retrieval/settings"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/bootstraphandlers"
	"github.com/SgtFlame/foundation/backend/features/secrets/rest/secretshandlers"
	bootstrapimpl "github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice/impl"
	secretsimpl "github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice/impl"
	"github.com/SgtFlame/foundation/backend/features/secrets/vaultstore"
	"github.com/SgtFlame/foundation/backend/features/tools"
	toolsbuiltin "github.com/SgtFlame/foundation/backend/features/tools/builtin"
	toolscontext "github.com/SgtFlame/foundation/backend/features/tools/context"
	toolsnodes "github.com/SgtFlame/foundation/backend/features/tools/nodes"
	toolssettings "github.com/SgtFlame/foundation/backend/features/tools/settings"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	"github.com/SgtFlame/foundation/backend/features/ui/hello"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/prefabhandlers"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/uihandlers"
	prefabimpl "github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice/impl"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
	uiimpl "github.com/SgtFlame/foundation/backend/features/ui/services/uiservice/impl"
	uisettings "github.com/SgtFlame/foundation/backend/features/ui/settings"
	uitools "github.com/SgtFlame/foundation/backend/features/ui/tools"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/rest/usagehandlers"
	usageimpl "github.com/SgtFlame/foundation/backend/features/usage/services/usageservice/impl"
	usagesettings "github.com/SgtFlame/foundation/backend/features/usage/settings"
	workbenchcontext "github.com/SgtFlame/foundation/backend/features/workbench/context"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
)

var (
	serveAddr       string
	serveKBRoot     string
	serveDataDir    string
	serveDB         string
	serveAppDB      string
	serveGeminiKey  string
	serveEmbedModel string
	serveEmbedDims  int
	serveChatModel  string
	serveLogFormat  string
	serveLogLevel   string
	serveVLogsURL   string
	serveVaultAddr  string
	serveVaultMount string
	serveVaultRole  string
	serveNatsURL    string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the REST API",
	Long: `Serve the REST API under /api/v1.

On startup serve migrates the relational database, opens the LanceDB index
and queues every volume file changed since it was last indexed. AI calls use
Gemini, with the API key from Settings (AI › Providers); --gemini-api-key or
$GEMINI_API_KEY wins over the stored key. Without a key AI calls fail with 503
and nothing is indexed, but everything else works. A key a user or tenant
sets in Settings is used for their own calls.

Without Vault, secret settings are encrypted with $FOUNDATION_SECRET_KEY, or
<data-dir>/secret.key, created on first use. With --vault-addr serve starts
sealed: it listens, but answers only /api/v1/bootstrap until an operator
runs backend/scripts/secrets/unseal.sh, then reads its database key and
provider keys from Vault and keeps secret settings there. See
documentation/Secrets.md and "cli secrets".

Until accounts are set up (the /setup page) serve runs in single-user mode,
with no sign-in. Afterwards every request needs a session. Tenants, users
and sessions live in the application database (--app-db); everything else
lives in the tenant database (--db). Each tenant has its own LanceDB
directory, <data-dir>/lancedb/<tenantId>.

The live core (commands, their events, projections, presence and UI
sessions) runs on NATS JetStream, embedded in <data-dir>/nats with no
network listener; --nats-url uses an external server instead. Browsers
hold one WebSocket each at /api/v1/live.

Logs go to stderr, and to VictoriaLogs when Settings (System › Logging)
turns it on; --vlogs-url or $FOUNDATION_VLOGS_URL wins over the setting.
backend/scripts/victorialogs.sh runs VictoriaLogs for development.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		level, err := logging.ParseLevel(serveLogLevel)
		if err != nil {
			return err
		}
		// Records are stored in VictoriaLogs once the logging settings give it a target
		victoria := logging.NewVictoria(logging.VictoriaOptions{})
		defer func() {
			closing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			victoria.Close(closing)
		}()
		if err := logging.Setup(os.Stderr, serveLogFormat, level, victoria.Handler()); err != nil {
			return err
		}
		log := logging.In("serve")
		vlogsURL := serveVLogsURL
		if vlogsURL == "" {
			vlogsURL = os.Getenv("FOUNDATION_VLOGS_URL")
		}
		vlogsTarget, err := targetFrom(vlogsURL)
		if err != nil {
			return err
		}
		victoria.SetTarget(vlogsTarget) // early, so startup is stored too

		// The server listens at once, behind a gate that serves only the
		// bootstrap routes until startup is done. With Vault it first waits
		// to be unsealed (documentation/Secrets.md › Sealed start)
		vaultAddr := serveVaultAddr
		if vaultAddr == "" {
			vaultAddr = os.Getenv("FOUNDATION_VAULT_ADDR")
		}
		roleID := serveVaultRole
		if roleID == "" {
			roleID = os.Getenv("FOUNDATION_VAULT_ROLE_ID")
		}
		boot, err := secrets.NewBootstrap(secrets.VaultOptions{Addr: vaultAddr, Mount: serveVaultMount, RoleID: roleID})
		if err != nil {
			return err
		}
		bootstrapService := bootstrapimpl.CreateService(bootstrapimpl.WithBootstrap(boot))
		bootstrapHandler := &bootstraphandlers.BootstrapHandler{}
		sealed := gin.New()
		sealed.Use(rest.Middleware(rest.WithAuthenticate(func(*http.Request) (features.Principal, error) { return features.AnonymousPrincipal(), nil })), gin.Recovery())
		bootstrapHandler.Register(sealed.Group("/api/v1"), bootstrapService)
		sealed.NoRoute(gin.WrapF(secrets.NotReady(boot)))
		gate := secrets.NewGate(boot, sealed)
		srv := &http.Server{Addr: serveAddr, Handler: gate}
		served := make(chan error, 1)
		go func() { served <- srv.ListenAndServe() }()
		if boot.Vault() {
			log.Info(ctx, "sealed: waiting for backend/scripts/secrets/unseal.sh to unseal the server (Vault at %s)", vaultAddr)
		}
		type unsealed struct {
			bundle map[string]string
			err    error
		}
		unsealing := make(chan unsealed, 1)
		go func() {
			bundle, err := boot.Wait(ctx)
			unsealing <- unsealed{bundle, err}
		}()
		var bundle map[string]string
		select {
		case err := <-served:
			return err
		case u := <-unsealing:
			if u.err != nil {
				return nil // stopped while sealed
			}
			bundle = u.bundle
		}

		dataDir := serveDataDir
		if dataDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			dataDir = filepath.Join(home, ".foundation")
		}
		dbPath := serveDB
		if dbPath == "" {
			dbPath = filepath.Join(dataDir, "foundation.db")
		}

		appDBPath := serveAppDB
		if appDBPath == "" {
			appDBPath = filepath.Join(dataDir, "app.db")
		}

		// The application database spans tenants (tenants, users, sessions);
		// the tenant database holds every tenant's rows, by TenantID
		appDB, err := db.Open(db.WithPath(appDBPath))
		if err != nil {
			return err
		}
		if err := db.Migrate(appDB, slices.Concat(identity.Models(), logs.Models(), authz.Models(), secrets.Models())...); err != nil {
			return err
		}
		gdb, err := db.Open(db.WithPath(dbPath))
		if err != nil {
			return err
		}
		if err := db.Migrate(gdb, slices.Concat(config.Models(), usage.Models(), conversation.Models(), retrieval.Models(), chronicle.Models(), cognition.Models(), tools.Models(), records.Models(), ui.Models(), apps.Models())...); err != nil { // features add their models here
			return err
		}

		// The live core's event streams, projections, presence and UI
		// sessions live in NATS JetStream, embedded with no listener unless
		// --nats-url names an external server (documentation/Live.md)
		natsURL := serveNatsURL
		if natsURL == "" {
			natsURL = os.Getenv("FOUNDATION_NATS_URL")
		}
		nats, err := natsd.Start(ctx, natsd.Options{Dir: filepath.Join(dataDir, "nats"), URL: natsURL})
		if err != nil {
			return err
		}
		defer nats.Close() // drained after the HTTP server has shut down

		// The index's and Chronicle's stores are opened first: retrieval's
		// library and chronicle sources search them
		kbRoot := serveKBRoot
		if kbRoot == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			kbRoot = filepath.Join(home, ".foundation", "kb")
		}
		// Each tenant has its own LanceDB directory; tables from before
		// tenants move into the default tenant's once. Volumes, and so the
		// index, are the default tenant's for now
		lanceRoot := filepath.Join(dataDir, "lancedb")
		if err := db.MoveToTenantDir(lanceRoot, features.LocalTenantID); err != nil {
			return err
		}
		store, err := lancestore.Open(ctx, db.TenantDir(lanceRoot, features.LocalTenantID), serveEmbedDims)
		if err != nil {
			return err
		}
		defer store.Close()
		entries := entrystore.OpenTenants(lanceRoot, serveEmbedDims)
		defer entries.Close()
		// Searchable records are kept beside them, embedded once the embed
		// service exists (and a key lets it)
		var recordDeps recordsdata.Deps
		var recordEmbedder embedservice.Service
		recordSearch := recordssearch.New(lanceRoot, serveEmbedDims, func(args features.ServiceArgs, text, title string, query bool) ([]float32, error) {
			if recordEmbedder == nil {
				return nil, errors.New("embedding is not ready")
			}
			purpose := embedservice.PurposeDocument
			if query {
				purpose = embedservice.PurposeQuery
			}
			args.Meta = args.Meta.WithFeature("records-search")
			resp := recordEmbedder.Embed(embedservice.EmbedArgs{ServiceArgs: args, Texts: []string{text}, Purpose: purpose, Title: title})
			if resp.Common.Code != http.StatusOK || len(resp.Vectors) == 0 {
				return nil, fmt.Errorf("embedding failed with %d: %s", resp.Common.Code, resp.Common.Message)
			}
			return resp.Vectors[0], nil
		}, func() recordsdata.Deps { return recordDeps })
		defer recordSearch.Close()

		// Features contribute to each other's extension points here, in order.
		// The RAG Search and Chronicle providers need services that need
		// configuration, which needs the finished registry: they get them
		// once they exist.
		var retrievalService retrievalservice.Service
		var chronicleDeps chroniclecontext.Deps
		var kbToolDeps kbtools.Deps
		var chronicleToolDeps chronicletools.Deps
		var agentDeps toolsnodes.Deps
		var liveToolDeps livetools.Deps
		var draftDeps drafts.Deps
		var entityDeps entities.Deps
		var prefabDeps prefabs.Deps
		var appDeps manifests.Deps
		var installDeps installs.Deps
		var appService appservice.Service
		var recordService recordservice.Service
		var liveSubscriptions *subscribe.Subscriptions
		var liveNodeDeps livenodes.Deps
		var uiNodeDeps uinodes.Deps
		var livePresence *presence.Presence
		var uiService uiservice.Service
		var configLive configservice.Service
		var chatLive chatservice.Service
		var conversationLive conversationservice.Service
		var collabLive collabservice.Service
		registry := extensions.NewRegistry()
		registry.Declare(config.Sections, config.SectionSources, conversation.ContextProviders, conversation.RoundFollowers, retrieval.Sources, cognition.Nodes, cognition.Graphs, cognition.Types, cognition.Kinds, cognition.Conversions, tools.Tools, authz.ResourceTypes,
			live.Commands, live.Projectors, live.Queries, live.Calls, live.Extensions, ui.Components, ui.Apps, ui.Pages, ui.EditorKinds, ui.AppSources)
		// Each feature declares its resource types and their actions
		authz.Contribute(registry)
		identity.ContributeAccess(registry)
		config.ContributeAccess(registry)
		logs.ContributeAccess(registry)
		usage.ContributeAccess(registry)
		conversation.ContributeAccess(registry)
		chronicle.ContributeAccess(registry)
		retrieval.ContributeAccess(registry)
		cognition.ContributeAccess(registry)
		kbservice.ContributeAccess(registry)
		ui.ContributeAccess(registry)
		hello.ContributeAccess(registry)
		records.ContributeAccess(registry)
		ui.ContributePrefabAccess(registry)
		apps.ContributeAccess(registry)
		aisettings.Contribute(registry)
		logssettings.Contribute(registry)
		usagesettings.Contribute(registry)
		retrievalsettings.Contribute(registry)
		indexsource.Contribute(registry, store, kbRoot)
		chroniclesource.Contribute(registry, entries)
		recordssearch.Contribute(registry, func() *recordssearch.Index { return recordSearch })
		chroniclecontext.Contribute(registry, func() chroniclecontext.Deps { return chronicleDeps })
		retrievalcontext.Contribute(registry, func() retrievalservice.Service { return retrievalService })
		workbenchcontext.Contribute(registry)
		cognitionsettings.Contribute(registry)
		cognitionnodes.Contribute(registry)
		conversationnodes.Contribute(registry)
		chroniclenodes.Contribute(registry, func() chroniclesteps.Steps { return chronicleDeps.Steps })
		toolssettings.Contribute(registry)
		toolsbuiltin.Contribute(registry)
		kbtools.Contribute(registry, func() kbtools.Deps { return kbToolDeps })
		chronicletools.Contribute(registry, func() chronicletools.Deps { return chronicleToolDeps })
		toolscontext.Contribute(registry)
		toolsnodes.Contribute(registry, func() toolsnodes.Deps { return agentDeps })
		// The live core, server-driven UI and the Hello app; a command is
		// also a tool, so livetools runs after every feature's commands
		livesettings.Contribute(registry)
		recordssettings.Contribute(registry)
		uisettings.Contribute(registry)
		if err := uicomponents.Contribute(registry); err != nil {
			return err
		}
		// Blueprints: the graph kinds and nodes of the live core and the UI
		drafts.Contribute(registry, func() drafts.Deps { return draftDeps })
		livenodes.Contribute(registry, func() livenodes.Deps { return liveNodeDeps })
		uinodes.Contribute(registry)
		ui.ContributeOverlays(registry)
		hello.Contribute(registry)
		// Entity types: edited by entity.* commands, published into the
		// tenant database
		entities.Contribute(registry, func() entities.Deps { return entityDeps })
		recordstools.Contribute(registry, func() *subscribe.Subscriptions { return liveSubscriptions })
		// Records: changed by record.* commands, converted when a field's
		// type changes
		recordsdata.Contribute(registry, func() recordsdata.Deps { return recordDeps })
		recordsnodes.Contribute(registry, func() recordsdata.Deps { return recordDeps })
		recordstools.ContributeRecords(registry, func() recordservice.Service { return recordService })
		// Prefabs: edited by prefab.* commands, published into the tenant
		// database
		prefabs.Contribute(registry, func() prefabs.Deps { return prefabDeps })
		uitools.ContributePrefabs(registry, func() *subscribe.Subscriptions { return liveSubscriptions })
		// Apps: manifests edited by app.* commands, after the prefabs and
		// records they name
		manifests.Contribute(registry, func() manifests.Deps { return appDeps })
		installs.Contribute(registry, func() installs.Deps { return installDeps })
		ops.Contribute(registry) // the operation log: ops.log and ops.list
		apptools.Contribute(registry, func() *subscribe.Subscriptions { return liveSubscriptions })
		apptools.ContributeTransfer(registry, func() appservice.Service { return appService })
		presence.Contribute(registry, func() *presence.Presence { return livePresence })
		uitools.Contribute(registry, func() *subscribe.Subscriptions { return liveSubscriptions })
		livetools.Contribute(registry, func() livetools.Deps { return liveToolDeps })
		uihandlers.ContributeLive(registry, func() uiservice.Service { return uiService })
		confighandlers.ContributeLive(registry, func() configservice.Service { return configLive })
		chathandlers.ContributeLive(registry, func() chatservice.Service { return chatLive })
		conversationhandlers.ContributeLive(registry, func() conversationservice.Service { return conversationLive })
		collabhandlers.ContributeLive(registry, func() collabservice.Service { return collabLive })
		conversationsettings.Contribute(registry) // after every chat.contextProvider: it adds their defaults sections
		extensionsService := extensionsimpl.CreateService(extensionsimpl.WithRegistry(registry))

		// Access is checked against the grants from here on; slice 10's
		// administrator flag becomes grants once
		policy := authz.NewPolicy(appDB, registry)
		if err := identity.MigrateRoles(ctx, appDB, policy); err != nil {
			return err
		}
		authz.Install(policy)
		authzService := authzimpl.CreateService(authzimpl.WithPolicy(policy), authzimpl.WithMembers(func(tenant, user uint64) (bool, error) {
			var n int64
			err := appDB.Model(&identity.Membership{}).Where("tenant_id = ? AND user_id = ?", tenant, user).Count(&n).Error
			return n > 0, err
		}))

		// The database key seals the signing key (and, without Vault, the
		// secret settings): from Vault's bootstrap bundle, or the key file
		var cipher *config.Cipher
		if boot.Vault() {
			if bundle["db-key"] == "" {
				return log.Errorf("Vault has no server/db-key: run backend/scripts/secrets/init.sh")
			}
			cipher, err = config.CipherFrom(bundle["db-key"])
		} else {
			cipher, err = config.LoadCipher(dataDir)
		}
		if err != nil {
			return err
		}
		signer, err := identity.LoadSigner(appDB, cipher)
		if err != nil {
			return err
		}
		auth, err := identity.NewAuthenticator(appDB, signer)
		if err != nil {
			return err
		}
		if auth.SingleUser() {
			log.Info(ctx, "single-user mode: no sign-in until accounts are set up")
		}
		identityService := identityimpl.CreateService(identityimpl.WithDB(appDB), identityimpl.WithAuthenticator(auth))
		tenantService := tenantimpl.CreateService(tenantimpl.WithDB(appDB))
		userService := userimpl.CreateService(userimpl.WithDB(appDB))
		configChanges := config.NewChanges()
		geminiKey := serveGeminiKey
		if geminiKey == "" {
			geminiKey = os.Getenv("GEMINI_API_KEY")
		}
		// With Vault, secret settings live there (old sealed values still
		// open with the database key until "cli secrets migrate" moves them)
		secretStore := config.SecretStore(config.CipherStore{Cipher: cipher})
		if boot.Vault() {
			secretStore = vaultstore.New(boot.KV(), cipher, boot.Lost)
		}
		configService := configimpl.CreateService(
			configimpl.WithDB(gdb), configimpl.WithRegistry(registry), configimpl.WithSecrets(secretStore), configimpl.WithChanges(configChanges),
			configimpl.WithServerSecret(aisettings.ProvidersID, "gemini.apiKey", bundle["gemini-api-key"]), // under every stored key
			configimpl.WithEnv(aisettings.ProvidersID, "gemini.apiKey", geminiKey),                         // flags and env win for secrets
		)
		userName := func(id uint64) string {
			var u identity.User
			if appDB.Select("username", "display_name").First(&u, id).Error != nil {
				return ""
			}
			if u.DisplayName != "" {
				return u.DisplayName
			}
			return u.Username
		}
		secretsService := secretsimpl.CreateService(secretsimpl.WithDB(appDB), secretsimpl.WithBootstrap(boot), secretsimpl.WithConfig(configService), secretsimpl.WithUserName(userName))

		// Logs are stored where the logging settings say (or the flag, which
		// wins), and follow them when they change
		applyLogging := func() {
			settings, err := config.Read[logssettings.Logging](configService, features.BackgroundArgs(ctx), logssettings.ID)
			if err != nil {
				log.Error(ctx, err, "cannot read the logging settings")
				return
			}
			target := settings.Target()
			if vlogsTarget.Configured() {
				target = vlogsTarget
			}
			victoria.SetLevel(settings.Stored())
			victoria.SetTarget(target)
		}
		applyLogging()
		configChanges.Subscribe(func(c config.Changed) {
			if c.Section == logssettings.ID {
				go applyLogging()
			}
		})
		if t := victoria.Target(); t.Configured() {
			log.Info(ctx, "storing logs in VictoriaLogs at %s", t.URL)
		}
		logsService := logsimpl.CreateService(logsimpl.WithVictoria(victoria), logsimpl.WithConfig(configService), logsimpl.WithDB(appDB))

		fileEvents := volumeservice.NewFileEvents()
		var kbOpts []kbimpl.Option
		volumeOpts := []volumeimpl.Option{volumeimpl.WithFileEvents(fileEvents)}
		collabOpts := []collabimpl.Option{collabimpl.WithFileEvents(fileEvents)}
		indexOpts := []indeximpl.Option{indeximpl.WithFileEvents(fileEvents)}
		if serveKBRoot != "" {
			kbOpts = append(kbOpts, kbimpl.WithRoot(serveKBRoot))
			volumeOpts = append(volumeOpts, volumeimpl.WithRoot(serveKBRoot))
			collabOpts = append(collabOpts, collabimpl.WithRoot(serveKBRoot))
			indexOpts = append(indexOpts, indeximpl.WithRoot(serveKBRoot))
		}
		kbService := kbimpl.CreateService(kbOpts...)
		volumeService := volumeimpl.CreateService(volumeOpts...)
		collabService := collabimpl.CreateService(collabOpts...)

		geminiOpts := []gemini.Option{gemini.WithEmbedDimensions(serveEmbedDims)}
		if serveEmbedModel != "" {
			geminiOpts = append(geminiOpts, gemini.WithEmbedModel(serveEmbedModel))
		}
		if serveChatModel != "" {
			geminiOpts = append(geminiOpts, gemini.WithChatModel(serveChatModel))
		}
		// Each call's key is the ai.providers setting as its principal sees
		// it: their own, their tenant's, the server's (or the flag and env
		// var, which win). Calls with no principal, such as indexing, use
		// the default tenant's
		geminiOpts = append(geminiOpts, gemini.WithKeyFor(func(ctx context.Context) string {
			args := features.BackgroundArgs(ctx)
			if p, _, ok := features.CallFrom(ctx); ok {
				args.Principal = p
			}
			providers, err := config.Read[aisettings.Providers](configService, args, aisettings.ProvidersID)
			if err != nil {
				log.Error(ctx, err, "cannot read the AI provider settings")
			}
			return providers.Gemini.APIKey
		}))
		provider := gemini.New(geminiOpts...)
		if !provider.Configured() {
			log.Warn(ctx, "no Gemini API key for the server; AI calls fail with 503 and nothing is indexed until one is set in Settings (users may still set their own)")
		}

		// Every AI call is recorded and priced from the model catalog
		recorder := usage.NewRecorder(gdb, func(ctx context.Context) (aisettings.Models, error) {
			return config.Read[aisettings.Models](configService, features.BackgroundArgs(ctx), aisettings.ModelsID)
		}, usage.NewEvents(), func(ctx context.Context) (usagesettings.Capture, error) {
			return config.Read[usagesettings.Capture](configService, features.BackgroundArgs(ctx), usagesettings.CaptureID)
		})
		defer recorder.Close()
		usageService := usageimpl.CreateService(usageimpl.WithDB(gdb))
		embedService := embedimpl.CreateService(embedimpl.WithProvider(usage.EmbedProvider(provider, "gemini", recorder)))
		chatService := chatimpl.CreateService(chatimpl.WithProvider(usage.ChatProvider(provider, "gemini", recorder)), chatimpl.WithConfig(configService))
		conversationService := conversationimpl.CreateService(
			conversationimpl.WithDB(gdb), conversationimpl.WithChat(chatService), conversationimpl.WithConfig(configService), conversationimpl.WithRegistry(registry),
		)

		retrievalService = retrievalimpl.CreateService(
			retrievalimpl.WithDB(gdb), retrievalimpl.WithEmbedder(embedService), retrievalimpl.WithConfig(configService), retrievalimpl.WithRegistry(registry),
		)
		chronicleService := chronicleimpl.CreateService(chronicleimpl.WithDB(gdb), chronicleimpl.WithStore(entries), chronicleimpl.WithEmbedder(embedService),
			chronicleimpl.WithUserName(userName))
		// The Cognition Engine runs the built-in graphs, such as Chronicle's
		// Personal mode, which the chronicle provider and follower run
		cognitionEngine := engine.New(gdb, registry, configService)
		for _, g := range cognitionEngine.Graphs() {
			for _, p := range cognitionEngine.Resolve(g).Problems {
				log.With("graphId", g.ID, "nodeId", p.NodeID).Warn(ctx, "Cognition graph %s: %s %s", g.ID, p.Severity, p.Message)
			}
		}
		cognitionService := cognitionimpl.CreateService(cognitionimpl.WithDB(gdb), cognitionimpl.WithEngine(cognitionEngine))
		kbToolDeps = kbtools.Deps{Root: kbRoot, Retrieval: retrievalService, Volumes: volumeService, Collab: collabService}
		chronicleToolDeps = chronicletools.Deps{Retrieval: retrievalService, Entries: chronicleService}
		agentDeps = toolsnodes.Deps{DB: gdb, Chat: chatService, Config: configService, Registry: registry}
		chronicleDeps = chroniclecontext.Deps{
			Steps:   chroniclesteps.Steps{DB: gdb, Chat: chatService, Config: configService, Retriever: func() retrievalservice.Service { return retrievalService }},
			Engine:  cognitionEngine,
			Entries: chronicleService,
		}
		// Entries changed or never embedded (no key yet) are indexed now, and
		// again whenever the provider settings change
		reindexEntries := func() {
			resp := chronicleService.Reindex(chronicleservice.ReindexArgs{ServiceArgs: features.BackgroundArgs(ctx)})
			if resp.Indexed > 0 {
				log.Info(ctx, "indexed %d Chronicle entries", resp.Indexed)
			}
		}
		go reindexEntries()
		configChanges.Subscribe(func(c config.Changed) {
			if c.Section == aisettings.ProvidersID {
				go reindexEntries()
			}
		})
		configLive, chatLive, conversationLive, collabLive = configService, chatService, conversationService, collabService

		// Commands decide on the projections; queries are subscriptions
		projections := project.New(nats.JS(), registry)
		defer projections.Close()
		liveBus := bus.New(projections)
		subscriptions := subscribe.New(projections, registry)
		livePresence = presence.New(nats.JS(), userName)
		// Graph drafts are edited by bp.* commands; the engine runs their
		// published versions
		cognitionEngine.SetSource(drafts.Source{Store: projections})
		draftDeps = drafts.Deps{Engine: cognitionEngine, Projections: projections}
		// Published command and query graphs are commands and queries like
		// any other; handlers that are graphs run in the UI sessions
		liveNodeDeps = livenodes.Deps{Engine: cognitionEngine, Bus: liveBus, Subscriptions: subscriptions, Store: projections, Presence: livePresence}
		uiNodeDeps = uinodes.Deps{Engine: cognitionEngine, Presence: livePresence}
		// An installed app's commands and queries are <appId>.<name>, run by
		// the graphs its version pinned; a manifest app's own graphs are
		// live only through its install
		liveBus.Resolve = installs.CommandFor(func() installs.Deps { return installDeps }, livenodes.CommandFor(func() livenodes.Deps { return liveNodeDeps }))
		subscriptions.Resolve = installs.QueryFor(func() installs.Deps { return installDeps }, livenodes.QueryFor(func() livenodes.Deps { return liveNodeDeps }))
		liveToolDeps = livetools.Deps{Bus: liveBus, Subscriptions: subscriptions}
		liveSubscriptions = subscriptions
		recordEmbedder = embedService
		recordFiles := recordsfiles.New(nats.JS())
		recordDeps = recordsdata.Deps{Projections: projections, Bus: liveBus, Files: recordFiles, Search: recordSearch, Settings: func(args features.ServiceArgs) recordssettings.Records {
			settings, err := config.Read[recordssettings.Records](configService, args, recordssettings.ID)
			if err != nil {
				return recordssettings.Defaults
			}
			return settings
		}, Users: func(tenant uint64, ids []uint64) map[uint64]recordsdata.User {
			var users []identity.User
			appDB.Select("users.id", "users.username", "users.display_name").
				Joins("JOIN memberships ON memberships.user_id = users.id AND memberships.tenant_id = ?", tenant).
				Where("users.id IN ?", ids).Find(&users)
			out := map[uint64]recordsdata.User{}
			for _, u := range users {
				out[u.ID] = recordsdata.User{Username: u.Username, DisplayName: u.DisplayName}
			}
			return out
		}}
		entityDeps = entities.Deps{Projections: projections, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return recordsdata.Follow(args, recordDeps) }}
		recordService = recordimpl.CreateService(recordimpl.WithRecords(liveBus, subscriptions, recordDeps), recordimpl.WithFiles(recordFiles))
		entityService := entityimpl.CreateService(entityimpl.WithEntities(entityDeps))
		prefabDeps = prefabs.Deps{Projections: projections, DB: gdb}
		appDeps = manifests.Deps{Projections: projections, DB: gdb}
		installDeps = installs.Deps{Projections: projections, DB: gdb, Nodes: liveNodeDeps, Roles: policy, Config: configService}
		appService = appimpl.CreateService(appimpl.WithDeps(func() appimpl.Deps {
			return appimpl.Deps{Projections: projections, DB: gdb, Bus: liveBus, Subscriptions: subscriptions, Registry: registry}
		}))
		// An installed app's own entity types are under its path (forge 05c)
		records.SetPlacement(installs.Placement(func() installs.Deps { return installDeps }))
		prefabService := prefabimpl.CreateService(prefabimpl.WithPrefabs(prefabDeps), prefabimpl.WithRegistry(registry), prefabimpl.WithSubscriptions(liveSubscriptions))
		uiService = uiimpl.CreateService(uiimpl.WithRegistry(registry), uiimpl.WithBus(liveBus), uiimpl.WithSubscriptions(subscriptions), uiimpl.WithStore(ui.NewKVSessions(nats.JS())),
			uiimpl.WithGraphs(uinodes.Runner{Deps: func() uinodes.Deps { return uiNodeDeps }}))
		liveService := liveimpl.CreateService(liveimpl.WithBus(liveBus), liveimpl.WithSubscriptions(subscriptions), liveimpl.WithPresence(livePresence), liveimpl.WithRegistry(registry),
			liveimpl.WithMaxSubscriptions(func() int {
				settings, err := config.Read[livesettings.Live](configService, features.BackgroundArgs(ctx), livesettings.ID)
				if err != nil || settings.MaxSubscriptions <= 0 {
					return livesettings.Defaults.MaxSubscriptions
				}
				return settings.MaxSubscriptions
			}))

		indexOpts = append(indexOpts, indeximpl.WithEmbedder(embedService), indeximpl.WithStore(store))
		indexService := indeximpl.CreateService(indexOpts...)

		// rest.Middleware finds each request's principal and logs it in place
		// of gin's logger. Only the identity routes work signed out; Volumes
		// work only in the default tenant
		router := gin.New()
		router.Use(rest.Middleware(rest.WithAuthenticate(auth.Authenticate)), gin.Recovery())
		public := router.Group("/api/v1")
		api := public.Group("", rest.RequireUser())
		volumes := api.Group("", kbhandlers.InTenant())

		identityHandler := &identityhandlers.IdentityHandler{}
		identityHandler.Register(public, identityService)

		bootstrapHandler.Register(public, bootstrapService)

		secretsHandler := &secretshandlers.SecretsHandler{}
		secretsHandler.Register(api, secretsService)

		tenantHandler := &tenanthandlers.TenantHandler{}
		tenantHandler.Register(api, tenantService)

		userHandler := &userhandlers.UserHandler{}
		userHandler.Register(api, userService)

		authzHandler := &authzhandlers.AuthzHandler{}
		authzHandler.Register(api, authzService)

		extensionsHandler := &extensionshandlers.ExtensionsHandler{}
		extensionsHandler.Register(api, extensionsService)

		configHandler := &confighandlers.ConfigHandler{}
		configHandler.Register(api, configService)

		logsHandler := &logshandlers.LogsHandler{}
		logsHandler.Register(api, logsService)

		usageHandler := &usagehandlers.UsageHandler{}
		usageHandler.Register(api, usageService)

		kbHandler := &kbhandlers.KnowledgeBaseHandler{}
		kbHandler.Register(volumes, kbService)

		volumeHandler := &volumehandlers.VolumeHandler{}
		volumeHandler.Register(volumes, volumeService)

		collabHandler := &collabhandlers.CollabHandler{}
		collabHandler.Register(volumes, collabService)

		embedHandler := &embedhandlers.EmbedHandler{}
		embedHandler.Register(api, embedService)

		chatHandler := &chathandlers.ChatHandler{}
		chatHandler.Register(api, chatService)

		conversationHandler := &conversationhandlers.ConversationHandler{}
		conversationHandler.Register(api, conversationService)

		chronicleHandler := &chroniclehandlers.ChronicleHandler{}
		chronicleHandler.Register(api, chronicleService)

		cognitionHandler := &cognitionhandlers.CognitionHandler{}
		cognitionHandler.Register(api, cognitionService)

		retrievalHandler := &retrievalhandlers.RetrievalHandler{}
		retrievalHandler.Register(api, retrievalService)

		liveHandler := &livehandlers.LiveHandler{}
		liveHandler.Register(api, liveService)

		entityHandler := &entityhandlers.EntityHandler{}
		entityHandler.Register(api, entityService)

		recordHandler := &recordhandlers.RecordHandler{}
		recordHandler.Register(api, recordService)

		uiHandler := &uihandlers.UiHandler{}
		uiHandler.Register(api, uiService)

		prefabHandler := &prefabhandlers.PrefabHandler{}
		prefabHandler.Register(api, prefabService)
		appHandler := &apphandlers.AppHandler{}
		appHandler.Register(api, appService)

		indexHandler := &indexhandlers.IndexHandler{}
		indexHandler.Register(volumes, indexService)

		// Shut down cleanly on SIGTERM, so serve.sh restarts close LanceDB
		gate.Open(router)
		log.Info(ctx, "ready on %s", serveAddr)
		select {
		case err := <-served:
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdown); err != nil {
				return log.Wrapf(err, "shutdown")
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "address to listen on")
	serveCmd.Flags().StringVar(&serveKBRoot, "kb-root", "", "absolute knowledge base root directory (default ~/.foundation/kb)")
	serveCmd.Flags().StringVar(&serveDataDir, "data-dir", "", "directory of the database, the LanceDB index and (without Vault) secret.key (default ~/.foundation)")
	serveCmd.Flags().StringVar(&serveDB, "db", "", "SQLite tenant database file (default <data-dir>/foundation.db)")
	serveCmd.Flags().StringVar(&serveAppDB, "app-db", "", "SQLite application database file: tenants, users and sessions (default <data-dir>/app.db)")
	serveCmd.Flags().StringVar(&serveGeminiKey, "gemini-api-key", "", "Gemini API key (default $GEMINI_API_KEY); wins over the key stored in Settings")
	serveCmd.Flags().StringVar(&serveEmbedModel, "embed-model", gemini.DefaultEmbedModel, "Gemini embedding model")
	serveCmd.Flags().IntVar(&serveEmbedDims, "embed-dims", gemini.DefaultEmbedDimensions, "embedding dimensions; changing it reindexes everything")
	serveCmd.Flags().StringVar(&serveChatModel, "chat-model", gemini.DefaultChatModel, "Gemini chat model for calls that name no level; Chat uses the levels in Settings")
	serveCmd.Flags().StringVar(&serveLogFormat, "log-format", "text", "log output on stderr: text or json")
	serveCmd.Flags().StringVar(&serveLogLevel, "log-level", "info", "lowest level logged on stderr: debug, info, warn or error")
	serveCmd.Flags().StringVar(&serveVaultAddr, "vault-addr", "", "HashiCorp Vault holding the server's secrets, e.g. http://127.0.0.1:8200 (default $FOUNDATION_VAULT_ADDR); serve then starts sealed")
	serveCmd.Flags().StringVar(&serveVaultMount, "vault-mount", "foundation", "Vault KV v2 mount of the server's secrets")
	serveCmd.Flags().StringVar(&serveVaultRole, "vault-role-id", "", "the only AppRole role ID an unseal may use (default $FOUNDATION_VAULT_ROLE_ID); without it unseal.sh sends it")
	serveCmd.Flags().StringVar(&serveNatsURL, "nats-url", "", "an external NATS server with JetStream for the live core, e.g. nats://127.0.0.1:4222 (default $FOUNDATION_NATS_URL); without it NATS runs embedded in <data-dir>/nats")
	serveCmd.Flags().StringVar(&serveVLogsURL, "vlogs-url", "", "VictoriaLogs to store logs in, e.g. http://localhost:9428 (default $FOUNDATION_VLOGS_URL); wins over Settings")
}

// targetFrom reads a VictoriaLogs URL, whose user info becomes its
// credentials; the empty URL is no target.
func targetFrom(raw string) (logging.Target, error) {
	if raw == "" {
		return logging.Target{}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return logging.Target{}, logging.In("serve").Errorf("--vlogs-url %q is not an http(s) URL", raw)
	}
	t := logging.Target{Username: u.User.Username()}
	t.Password, _ = u.User.Password()
	u.User = nil
	t.URL = u.String()
	return t, nil
}
