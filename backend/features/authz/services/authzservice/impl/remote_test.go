package impl

import (
	"context"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	authzproxy "github.com/SgtFlame/foundation/backend/features/authz/services/authzservice/proxy"
	authzserver "github.com/SgtFlame/foundation/backend/features/authz/services/authzservice/server"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/services"
)

// A grant changed in the process holding the application database
// reaches the policy of another process within a second (forge 21d-1).
func TestGrantsReachAnotherProcess(t *testing.T) {
	ns := natsd.StartTestListening(t)
	gdb, err := db.Open(db.WithPath(t.TempDir() + "/app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "tenant"}, Parent: "app",
		Actions: []authz.Action{{Name: "read", Class: authz.ClassRead}},
	})

	// The owner: the database, authz's data service, announcing changes.
	owner := authz.NewPolicy(gdb, reg)
	oc, err := ns.Connect("owner")
	if err != nil {
		t.Fatal(err)
	}
	owner.Broadcast(oc)
	keys, err := services.NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := authzserver.Serve(oc, newService(WithPolicy(owner)), services.VerifyWith(keys.Public()))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	if err := oc.Flush(); err != nil {
		t.Fatal(err)
	}

	// Another process: grants through the proxy, caches dropped on change.
	rc, err := ns.Connect("remote")
	if err != nil {
		t.Fatal(err)
	}
	remote := authz.NewPolicyFrom(authzservice.Remote(authzproxy.New(&services.Client{NC: rc, Caller: "remote", Keys: keys}), "authz"), reg)
	if _, err := remote.Follow(rc); err != nil {
		t.Fatal(err)
	}
	if err := rc.Flush(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	user := features.Principal{Kind: features.PrincipalUser, UserID: 2, TenantID: 1}
	if d := remote.Decide(ctx, user, "tenant.read", "tenant:1"); d.Allowed {
		t.Fatalf("allowed before the grant: %+v", d)
	}
	if _, err := owner.Grant(ctx, nil, authz.GrantArgs{UserID: 2, RoleID: authz.RoleViewer, Resource: "tenant:1"}); err != nil {
		t.Fatal(err)
	}
	owner.Changed()
	deadline := time.Now().Add(time.Second)
	for !remote.Decide(ctx, user, "tenant.read", "tenant:1").Allowed {
		if time.Now().After(deadline) {
			t.Fatal("the grant never reached the other process")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A person cannot read grants this way.
	resp := newService(WithPolicy(owner)).ReadGrants(authzservice.ReadGrantsArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: user}, ID: 2})
	if resp.Common.Code != 403 {
		t.Fatalf("a person read grants: %+v", resp.Common)
	}
}
