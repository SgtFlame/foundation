package volumeservice

import (
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
)

// Resource is the access path of dir: the Volume holding it
// ("tenant:1/volume:<id>"), or the knowledge base's folders outside any
// Volume ("tenant:1/kb"). Grants on a Volume cover its files and folders.
func Resource(p features.Principal, root, dir string) string {
	for ; ; dir = filepath.Dir(dir) {
		if marker, err := ReadMarker(dir); err == nil {
			return authz.Tenant(p) + "/" + authz.Resource("volume", marker.ID)
		}
		if dir == root || dir == filepath.Dir(dir) {
			return authz.Tenant(p) + "/kb"
		}
	}
}

// Check checks read or write ("read"/"write") on dir: volume.<access> inside
// a Volume, kb.<access> outside one. It fails with authz.Status.
func Check(args features.ServiceArgs, root, dir, access string) *features.ServiceResponse {
	res := Resource(args.Principal, root, dir)
	action := "kb." + access
	if filepath.Base(res) != "kb" {
		action = "volume." + access
	}
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// May tells whether the principal has access ("read"/"write") to dir,
// without failing.
func May(args features.ServiceArgs, root, dir, access string) bool {
	res := Resource(args.Principal, root, dir)
	action := "kb." + access
	if filepath.Base(res) != "kb" {
		action = "volume." + access
	}
	return authz.Check(args.Context(), args.Principal, action, res).Allowed
}
