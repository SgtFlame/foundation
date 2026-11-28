package wiring

import (
	foundation "github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	gitconnector "github.com/SgtFlame/foundation/backend/features/connectors/git"
	gitgithub "github.com/SgtFlame/foundation/backend/features/connectors/git/github"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/gittest"
	githttps "github.com/SgtFlame/foundation/backend/features/connectors/git/https"
	gitnodes "github.com/SgtFlame/foundation/backend/features/connectors/git/nodes"
	gitssh "github.com/SgtFlame/foundation/backend/features/connectors/git/ssh"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Git (forge 24): git.repo@1, git.review@1 and git.key@1, the Git test
// server with its control and log calls, the git.github, git.https and
// git.ssh types, and the git nodes and tools (through the dispatcher, as
// the caller), which keep mirrored Volumes.
func init() {
	Register(Feature{
		Name: "git",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			gitconnector.Contribute(reg)
			gittest.Contribute(reg)
			gitgithub.Contribute(reg)
			githttps.Contribute(reg)
			gitssh.Contribute(reg)
			gittest.ContributeLive(reg, func() *connectors.Dispatcher { return env.Connectors().Dispatcher })
			deps := func() gitnodes.Deps {
				d := gitnodes.Deps{Repositories: func(a foundation.ServiceArgs) ([]gitnodes.Repository, error) {
					views, err := connections.List(a, env.Connections(), "", gitconnector.RepoRef)
					out := make([]gitnodes.Repository, 0, len(views))
					for _, v := range views {
						out = append(out, gitnodes.Repository{ID: v.ID, Name: v.Name, Type: v.Type, Status: v.Status, Settings: v.Settings})
					}
					return out, err
				}}
				if m := env.VolumeMirrors(); m != nil {
					d.Mirrors = *m
				}
				if dp := env.Connectors().Dispatcher; dp != nil {
					d.Call = dp.Call
				}
				return d
			}
			gitnodes.Contribute(reg, deps)
			gitnodes.ContributeTools(reg, deps)
			return nil
		},
		Adapter: func(o *adapter.Options) {
			o.Types = append(o.Types, gittest.Type(), gitgithub.Type(), githttps.Type(), gitssh.Type())
			o.Contracts = append(append(o.Contracts, gitconnector.Contracts()...), gittest.ControlContract())
			o.Impls = append(o.Impls, gittest.Default.Impl(),
				gitgithub.Impl(gitgithub.Options{Files: gitconnector.ServerFiles}),
				githttps.Impl(githttps.Options{Files: gitconnector.ServerFiles}),
				gitssh.Impl(gitssh.Options{Files: gitconnector.ServerFiles}))
		},
	})
}
