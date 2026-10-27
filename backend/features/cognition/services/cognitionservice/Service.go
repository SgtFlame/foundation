package cognitionservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
)

// Service serves Cognition graphs and their run traces to the Cognition
// Editor (documentation/plans/Cognition.md › Services and REST). Slice 7
// serves the built-in graphs read-only; drafts, publishing and runs from
// the editor come later. Runs are private to the principal who caused them.
type Service interface {
	ListGraphs(args ListGraphsArgs) ListGraphsResponse
	GetGraph(args GetGraphArgs) GetGraphResponse
	NodeTypes(args NodeTypesArgs) NodeTypesResponse
	ListRuns(args ListRunsArgs) ListRunsResponse
	GetTrace(args GetTraceArgs) GetTraceResponse
}

type ListGraphsArgs struct {
	features.ServiceArgs
}

// GraphInfo is a graph without its nodes.
type GraphInfo struct {
	ID          string
	Name        string
	Description string
	Kind        string
	Version     int
	BuiltIn     bool // read-only
}

type ListGraphsResponse struct {
	Common features.ServiceResponse
	Graphs []GraphInfo // by kind, then name
}

type GetGraphArgs struct {
	features.ServiceArgs
	ID string
}

type GetGraphResponse struct {
	Common   features.ServiceResponse
	Info     GraphInfo
	Graph    cognition.Graph
	Pins     map[string]cognition.Pins // each node's pins, by node ID
	Problems []cognition.Problem
}

type NodeTypesArgs struct {
	features.ServiceArgs
}

type NodeTypesResponse struct {
	Common    features.ServiceResponse
	NodeTypes []cognition.NodeType // with their fixed Pins; nodes whose pins follow their config get them from GetGraph
	Types     []cognition.DataType
}

// ListRunsArgs pages through the principal's runs of a graph, newest first.
type ListRunsArgs struct {
	features.ServiceArgs
	GraphID        string
	ConversationID uint64 // only this conversation's runs, when set
	Before         uint64 // only runs older than this one, when set
	Limit          int    // 50 by default, at most 200
}

type ListRunsResponse struct {
	Common features.ServiceResponse
	Runs   []cognition.Run
	More   bool
}

type GetTraceArgs struct {
	features.ServiceArgs
	RunID uint64
}

// NodeUsage is what a node's model calls used in a run.
type NodeUsage struct {
	NodeID         string
	Calls          int
	InputTokens    int
	OutputTokens   int
	ThinkingTokens int
	CostMicros     int64
	Currency       string
}

type GetTraceResponse struct {
	Common features.ServiceResponse
	Run    cognition.Run
	Steps  []cognition.Step // in the order they ran
	Usage  []NodeUsage      // by node; usage is recorded within a second or so of the call
}
