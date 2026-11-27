// Package retrievaltools contributes the retrieval.vote tool and the Vote
// on segments node (forge 15b): a reviewer's outcome on what an agent
// wrote counts as 👍 or 👎 on the segments it cited, once per key.
package retrievaltools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ID is the tool's and the node's id.
const ID = "retrieval.vote"

// VoteArgs are what a vote takes.
type VoteArgs struct {
	Segments []retrieval.SegmentRef `json:"segments"`
	Verdict  string                 `json:"verdict"`
	Key      string                 `json:"key"`
}

const input = `{"type":"object","required":["segments","verdict","key"],"properties":{
"segments":{"type":"array","description":"The segments to vote on, as a reply cited them","items":{"type":"object","required":["sourceId","key"],"properties":{
"sourceId":{"type":"string","description":"The retrieval source, such as library or chronicle"},
"key":{"type":"string","description":"The segment's feedback key"}}}},
"verdict":{"type":"string","enum":["up","down"],"description":"up: the segments served well; down: they did not"},
"key":{"type":"string","description":"What makes the vote once, such as an agent run's id: a key that has voted changes nothing"}}}`

// Vote casts a vote as args' principal through the retrieval data
// service (forge 21d-2): retrieval.vote on the tenant, then VoteOnce. It
// says whether the vote counted.
func Vote(svc retrievalservice.Service, args features.ServiceArgs, v VoteArgs) (bool, error) {
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, "retrieval.vote", resource).Err("retrieval.vote", resource); err != nil {
		return false, err
	}
	// A sandboxed or dry run counts nothing (read here: the context does
	// not travel)
	if _, sandboxed := engine.SandboxOf(args.Context()); sandboxed || bus.DryOf(args.Context()) != nil {
		return false, nil
	}
	if svc == nil {
		return false, fmt.Errorf("votes cannot be stored here")
	}
	resp := svc.VoteOnce(retrievalservice.VoteOnceArgs{ServiceArgs: args, Key: v.Key, Segments: v.Segments, Verdict: v.Verdict})
	if resp.Common.Code != 200 {
		return false, errors.New(resp.Common.Message)
	}
	return resp.Cast, nil
}

// Contribute registers the tool and the node; svc gives the retrieval
// service (a proxy in a process without the tenant database).
func Contribute(reg *extensions.Registry, svc func() retrievalservice.Service) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 400, Source: "retrieval"},
		Title:       "Vote on segments",
		Description: "Counts a verdict on the segments a reply cited, as 👍 or 👎 on a chat reply does, tenant-wide: up when they served well, down when they did not. Each key votes once, so repeating it changes nothing.",
		Input:       json.RawMessage(input),
		Effect:      tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			v, err := tools.Decode[VoteArgs](call)
			if err != nil {
				return tools.Result{}, err
			}
			cast, err := Vote(svc(), call.ServiceArgs, v)
			if err != nil {
				return tools.Result{}, err
			}
			summary := fmt.Sprintf("Voted %s on %d segments", v.Verdict, len(v.Segments))
			if !cast {
				summary = "Already voted with key " + v.Key
			}
			return tools.Result{Content: map[string]any{"voted": cast}, Summary: summary}, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 360, Source: "retrieval"},
		Title:       "Vote on segments",
		Category:    "Tools",
		Description: "Counts a verdict on segments, as 👍 or 👎 on a chat reply does, tenant-wide, once per key: a key that has voted changes nothing (forge 15b: a reviewer's outcome on an agent's draft, keyed by the run). Needs retrieval.vote, which an app's principal holds in its tenant. Leaves by Failed with the error when it may not.",
		Example:     "Vote on segments up with the draft's cited segments and the run's id as key, when the draft is sent unchanged",
		Tags:        []string{"retrieval", "feedback", "grounding", "agents"},
		Effects:     true,
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("segments", "Segments", "json", "The segments: [{sourceId, key}]"),
				cognition.DataPin("verdict", "Verdict", "string", "up or down"),
				cognition.DataPin("key", "Key", "string", "What makes it once, such as a run's id"),
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("voted", "Voted", "bool", "Whether the vote counted: false when the key had voted"),
				cognition.DataPin("error", "Error", "string", "Why it failed, when it did"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var v VoteArgs
			var err error
			if v.Segments, err = cognition.Get[[]retrieval.SegmentRef](in, "segments"); err != nil {
				return nil, "", err
			}
			v.Verdict, _ = cognition.Get[string](in, "verdict")
			v.Key, _ = cognition.Get[string](in, "key")
			cast, err := Vote(svc(), x.ServiceArgs, v)
			if err != nil {
				return cognition.Values{"voted": false, "error": err.Error()}, "failed", nil
			}
			return cognition.Values{"voted": cast, "error": ""}, cognition.Then, nil
		},
	})
}
