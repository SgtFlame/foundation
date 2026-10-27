// Client for the backend Cognition REST handlers
// (backend/features/cognition/rest/cognitionhandlers)

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/cognition/rest/cognitiondto. A graph
// travels as its own JSON document (cognition.Graph). uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export type GraphKind = 'mode' | 'agent' | 'function' | 'summarizer'

export interface GraphInfo {
  id: string
  name: string
  description: string
  kind: GraphKind
  version: number
  builtIn: boolean // read-only
}

export interface PinRef {
  node: string
  pin: string
}

export interface GraphNode {
  id: string
  type: string // a node type id
  title?: string
  position: { x: number; y: number }
  config?: unknown
}

export interface Graph {
  id: string
  name: string
  description: string
  kind: GraphKind
  version: number
  nodes: GraphNode[]
  edges: { from: PinRef; to: PinRef }[]
}

export interface Pin {
  id: string
  name: string
  exec?: boolean // orders side effects; otherwise it carries type
  type?: string
  optional?: boolean
  description?: string
}

export interface Pins {
  in: Pin[]
  out: Pin[]
}

export interface Problem {
  severity: 'error' | 'warning'
  nodeId?: string
  pin?: string
  message: string
}

export interface NodeType {
  id: string
  title: string
  category: string
  description: string
  color?: string
  source: string
  event?: boolean
  config?: unknown // the config's schema
  pins: Pins
  pinsFromConfig?: boolean // its pins follow each node's config: GetGraph has them
}

export interface DataType {
  id: string
  color: string
  description: string
}

export type RunStatus = 'running' | 'ok' | 'failed'

export interface Run {
  id: string
  graphId: string
  graphVersion: number
  event: string // the event node's id
  trigger: string
  status: RunStatus
  error?: string
  failedNode?: string
  conversationId?: string
  roundId?: string
  startedAt: string // RFC 3339
  durationMs: number
  steps: number
  outputs?: Record<string, unknown>
}

// A pin value cut short in the trace
export interface Truncated {
  $truncated: number // its size in bytes
  preview: string
}

export interface Step {
  seq: number
  nodeId: string
  nodeType: string
  context?: string // the loop frames; absent outside loops
  inputs?: Record<string, unknown> // by pin id
  outputs?: Record<string, unknown>
  execPin?: string
  startedAt: string
  durationMs: number
  error?: string
}

export interface NodeUsage {
  nodeId: string
  calls: number
  inputTokens: number
  outputTokens: number
  thinkingTokens: number
  costMicros: number
  currency?: string
}

export interface ListGraphsResponse extends ResponseStatus {
  graphs: GraphInfo[]
}

export interface GetGraphResponse extends ResponseStatus {
  info?: GraphInfo
  graph?: Graph
  pins?: Record<string, Pins> // by node id
  problems?: Problem[]
}

export interface NodeTypesResponse extends ResponseStatus {
  nodeTypes: NodeType[]
  types: DataType[]
}

export interface ListRunsRequest {
  graphId: string
  conversationId?: string
  before?: string // only runs older than this one
  limit?: number // 50 by default, at most 200
}

export interface ListRunsResponse extends ResponseStatus {
  runs: Run[] // newest first
  more?: boolean
}

export interface GetTraceResponse extends ResponseStatus {
  run?: Run
  steps?: Step[]
  usage?: NodeUsage[]
}

export const Cognition = {
  async listGraphs(): Promise<ListGraphsResponse> {
    const response = await http.get<ListGraphsResponse>('/cognition/graphs')
    return response.data
  },

  async getGraph(id: string): Promise<GetGraphResponse> {
    const response = await http.post<GetGraphResponse>('/cognition/graph', { id })
    return response.data
  },

  async nodeTypes(): Promise<NodeTypesResponse> {
    const response = await http.get<NodeTypesResponse>('/cognition/node-types')
    return response.data
  },

  async listRuns(request: ListRunsRequest): Promise<ListRunsResponse> {
    const response = await http.post<ListRunsResponse>('/cognition/runs', request)
    return response.data
  },

  async getTrace(runId: string): Promise<GetTraceResponse> {
    const response = await http.post<GetTraceResponse>('/cognition/trace', { runId })
    return response.data
  },
}
