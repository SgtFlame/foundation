import { createContext, useContext } from 'react'
import type { DataType, GetTraceResponse, Graph, GraphInfo, NodeType, Pins, Problem, Run } from '../../services/Cognition.ts'

// What is selected in the editor: a node, and optionally one of its pins.
// The canvas, the Log Viewer and the details panel share it.
export interface Selection {
  nodeId?: string
  pin?: { id: string; dir: 'in' | 'out' }
  seq?: number // the step selected in the Log Viewer
}

// One node's execution in the selected run, for the canvas overlays
export interface NodeStats {
  count: number
  durationMs: number
  error?: string
  inputTokens: number
  outputTokens: number
  costMicros: number
  currency?: string
}

export interface CognitionState {
  info: GraphInfo
  graph: Graph
  pins: Record<string, Pins>
  problems: Problem[]
  nodeTypes: Map<string, NodeType>
  types: Map<string, DataType>
  runs: Run[]
  moreRuns: boolean
  runsError?: string
  loadMoreRuns: () => void
  refreshRuns: () => void
  runId?: string
  setRunId: (id: string | undefined) => void
  trace?: GetTraceResponse
  traceError?: string
  stats: Map<string, NodeStats> // by node id, for the selected run
  selection: Selection
  select: (selection: Selection) => void
}

export const CognitionContext = createContext<CognitionState | null>(null)

export function useCognition(): CognitionState {
  const state = useContext(CognitionContext)
  if (!state) throw new Error('useCognition outside the Cognition Editor')
  return state
}

// A node's title: its own, else its type's
export function titleOf(state: Pick<CognitionState, 'graph' | 'nodeTypes'>, nodeId: string): string {
  const node = state.graph.nodes.find((n) => n.id === nodeId)
  if (!node) return nodeId
  return node.title || state.nodeTypes.get(node.type)?.title || node.type
}

const categoryColors: Record<string, string> = {
  Events: '#b71c1c',
  Flow: '#455a64',
  Chronicle: '#6a1b9a',
  LLM: '#1565c0',
  Retrieval: '#00695c',
  Nexus: '#0d47a1',
}

// The header colour of a node type
export function headerColor(type: NodeType | undefined): string {
  return type?.color || categoryColors[type?.category ?? ''] || '#37474f'
}

// The colour of a data type; unknown types are grey
export function typeColor(types: Map<string, DataType>, type: string | undefined): string {
  return (type && types.get(type)?.color) || '#9e9e9e'
}

// Stats per node from a trace
export function statsOf(trace: GetTraceResponse | undefined): Map<string, NodeStats> {
  const stats = new Map<string, NodeStats>()
  for (const step of trace?.steps ?? []) {
    const s = stats.get(step.nodeId) ?? { count: 0, durationMs: 0, inputTokens: 0, outputTokens: 0, costMicros: 0 }
    s.count++
    s.durationMs += step.durationMs
    if (step.error) s.error = step.error
    stats.set(step.nodeId, s)
  }
  for (const u of trace?.usage ?? []) {
    const s = stats.get(u.nodeId)
    if (!s) continue
    s.inputTokens += u.inputTokens
    s.outputTokens += u.outputTokens + u.thinkingTokens
    s.costMicros += u.costMicros
    s.currency = u.currency
  }
  return stats
}

// The nodes a run from the event node can execute: the exec flow from it,
// and the pure nodes those pull from
export function reachedBy(graph: Graph, pins: Record<string, Pins>, event: string): Set<string> {
  const reached = new Set<string>()
  const isExec = (node: string, pin: string) => !!pins[node]?.out.find((p) => p.id === pin)?.exec
  const pure = (node: string) => ![...(pins[node]?.in ?? []), ...(pins[node]?.out ?? [])].some((p) => p.exec)
  const visit = (node: string) => {
    if (reached.has(node)) return
    reached.add(node)
    for (const e of graph.edges) {
      if (e.from.node === node && isExec(node, e.from.pin)) visit(e.to.node)
      if (e.to.node === node && !isExec(e.from.node, e.from.pin) && pure(e.from.node)) visit(e.from.node)
    }
  }
  visit(event)
  return reached
}
