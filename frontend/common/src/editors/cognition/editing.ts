// Editing a graph's draft over the live core (documentation/Cognition.md ›
// Built in slice 14): the draft is the cognition.graph query, every gesture
// is one or more bp.* commands, shown at once by applying them over the
// query's data until it reflects them, and undone by this user's own stack
// of inverse commands, so each person undoes only their changes.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { CommandRejected, live, useCommand, useQuery, type PendingCommand } from '../../live/index.ts'
import type { Edge, GraphNode, GraphView, NodeType, Pins, Variable } from '../../services/Cognition.ts'
import type { ServiceError } from '../../services/http/client.ts'

// One bp.* command on the graph
export interface Op {
  command: string
  args: Record<string, unknown>
}

const sameEdge = (a: Edge, b: Edge) => a.from.node === b.from.node && a.from.pin === b.from.pin && a.to.node === b.to.node && a.to.pin === b.to.pin

function isExecOut(pins: Record<string, Pins>, e: Edge) {
  return !!pins[e.from.node]?.out.find((p) => p.id === e.from.pin)?.exec
}

// The edges a connection replaces: an exec output leads to one node, a
// data input takes one edge (bp.connect)
export function replacedBy(view: GraphView, e: Edge): Edge[] {
  const exec = isExecOut(view.pins, e)
  return view.graph.edges.filter((old) => !sameEdge(old, e) && (exec ? old.from.node === e.from.node && old.from.pin === e.from.pin : old.to.node === e.to.node && old.to.pin === e.to.pin))
}

// Applies a command the way the server will, so the gesture shows at once
export function applyOp(view: GraphView, op: Op, nodeTypes: Map<string, NodeType>): GraphView {
  const g = view.graph
  const a = op.args
  const next = (patch: Partial<GraphView['graph']>, pins = view.pins): GraphView => ({ ...view, graph: { ...g, ...patch }, pins })
  switch (op.command) {
    case 'bp.moveNodes': {
      const moves = new Map((a.moves as { node: string; position: GraphNode['position'] }[]).map((m) => [m.node, m.position]))
      return next({ nodes: g.nodes.map((n) => (moves.has(n.id) ? { ...n, position: moves.get(n.id)! } : n)) })
    }
    case 'bp.addNode': {
      if (!a.id) return view // the server picks the id
      const node: GraphNode = { id: a.id as string, type: a.type as string, title: a.title as string | undefined, config: a.config, position: (a.position as GraphNode['position']) ?? { x: 0, y: 0 } }
      const type = nodeTypes.get(node.type)
      return next({ nodes: [...g.nodes, node] }, type && !type.pinsFromConfig ? { ...view.pins, [node.id]: type.pins } : view.pins)
    }
    case 'bp.setConfig':
      return next({
        nodes: g.nodes.map((n) => (n.id === a.node ? { ...n, ...(a.config !== undefined ? { config: a.config } : {}), ...(a.title !== undefined ? { title: a.title as string } : {}) } : n)),
      })
    case 'bp.connect': {
      const e = a as unknown as Edge
      const replaced = replacedBy(view, e)
      return next({ edges: [...g.edges.filter((x) => !replaced.some((r) => sameEdge(r, x)) && !sameEdge(x, e)), e] })
    }
    case 'bp.disconnect':
      return next({ edges: g.edges.filter((x) => !sameEdge(x, a as unknown as Edge)) })
    case 'bp.remove': {
      const nodes = new Set((a.nodes as string[] | undefined) ?? [])
      const edges = (a.edges as Edge[] | undefined) ?? []
      return next({
        nodes: g.nodes.filter((n) => !nodes.has(n.id)),
        edges: g.edges.filter((x) => !nodes.has(x.from.node) && !nodes.has(x.to.node) && !edges.some((r) => sameEdge(r, x))),
      })
    }
    case 'bp.paste': {
      const nodes = a.nodes as GraphNode[]
      const pins = { ...view.pins }
      for (const n of nodes) {
        const t = nodeTypes.get(n.type)
        if (t && !t.pinsFromConfig) pins[n.id] = t.pins
      }
      return next({ nodes: [...g.nodes, ...nodes], edges: [...g.edges, ...((a.edges as Edge[]) ?? [])] }, pins)
    }
    case 'bp.addVariable': {
      const v = a as unknown as Variable
      const vars = g.variables ?? []
      return next({ variables: vars.some((x) => x.id === v.id) ? vars.map((x) => (x.id === v.id ? v : x)) : [...vars, v] })
    }
    case 'bp.removeVariable':
      return next({ variables: (g.variables ?? []).filter((x) => x.id !== a.id) })
    case 'bp.setInfo':
      return next({ ...(a as Partial<GraphView['graph']>) })
  }
  return view
}

// The commands that undo op, from the draft as it is before op
export function inverse(view: GraphView, op: Op): Op[] {
  const g = view.graph
  const a = op.args
  switch (op.command) {
    case 'bp.moveNodes': {
      const moves = (a.moves as { node: string }[]).flatMap((m) => {
        const n = g.nodes.find((x) => x.id === m.node)
        return n ? [{ node: n.id, position: n.position }] : []
      })
      return [{ command: 'bp.moveNodes', args: { moves } }]
    }
    case 'bp.addNode':
      return a.id ? [{ command: 'bp.remove', args: { nodes: [a.id] } }] : []
    case 'bp.setConfig': {
      const n = g.nodes.find((x) => x.id === a.node)
      if (!n) return []
      return [{ command: 'bp.setConfig', args: { node: n.id, ...(a.config !== undefined ? { config: n.config ?? {} } : {}), ...(a.title !== undefined ? { title: n.title ?? '' } : {}) } }]
    }
    case 'bp.connect': {
      const e = a as unknown as Edge
      if (g.edges.some((x) => sameEdge(x, e))) return []
      return [{ command: 'bp.disconnect', args: { ...e } }, ...replacedBy(view, e).map((r) => ({ command: 'bp.connect', args: { ...r } }))]
    }
    case 'bp.disconnect':
      return [{ command: 'bp.connect', args: { ...a } }]
    case 'bp.remove': {
      const ids = new Set((a.nodes as string[] | undefined) ?? [])
      const nodes = g.nodes.filter((n) => ids.has(n.id))
      const removed = (a.edges as Edge[] | undefined) ?? []
      const edges = g.edges.filter((x) => ids.has(x.from.node) || ids.has(x.to.node))
      const loose = removed.filter((r) => g.edges.some((x) => sameEdge(x, r)) && !edges.some((x) => sameEdge(x, r)))
      const out: Op[] = []
      if (nodes.length) out.push({ command: 'bp.paste', args: { nodes, edges } })
      for (const e of loose) out.push({ command: 'bp.connect', args: { ...e } })
      return out
    }
    case 'bp.paste': {
      const nodes = (a.nodes as GraphNode[]).map((n) => n.id)
      return [{ command: 'bp.remove', args: { nodes } }]
    }
    case 'bp.addVariable': {
      const old = (g.variables ?? []).find((x) => x.id === a.id)
      return [old ? { command: 'bp.addVariable', args: { ...old } } : { command: 'bp.removeVariable', args: { id: a.id } }]
    }
    case 'bp.removeVariable': {
      const old = (g.variables ?? []).find((x) => x.id === a.id)
      return old ? [{ command: 'bp.addVariable', args: { ...old } }] : []
    }
    case 'bp.setInfo': {
      const back: Record<string, unknown> = {}
      for (const k of Object.keys(a)) back[k] = (g as unknown as Record<string, unknown>)[k] ?? (k === 'spec' ? {} : '')
      return [{ command: 'bp.setInfo', args: back }]
    }
  }
  return []
}

interface Entry {
  redo: Op[]
  undo: Op[]
}

export interface DraftEditing {
  view?: GraphView // the draft with this user's pending commands applied
  loading: boolean
  error?: ServiceError
  // Sends one gesture's commands, undoable as one
  apply(ops: Op[]): void
  // Sends a command that is not undone, such as bp.publish
  send(command: string, args: Record<string, unknown>): Promise<void>
  undo(): void
  redo(): void
  canUndo: boolean
  canRedo: boolean
  // The last command refused, for the editor to show
  rejection?: string
  clearRejection(): void
}

function message(err: unknown): string {
  if (err instanceof CommandRejected) return err.problems.map((p) => p.message + (p.fix ? ` (${p.fix})` : '')).join('; ') || err.message
  return err instanceof Error ? err.message : String(err)
}

// The draft of a graph and its editing, for one user in one editor
export function useDraft(graphId: string, nodeTypes: Map<string, NodeType> | undefined): DraftEditing {
  const q = useQuery<GraphView>('cognition.graph', { id: graphId })
  const commands = useCommand()
  const [stacks, setStacks] = useState<{ graphId: string; done: Entry[]; undone: Entry[] }>({ graphId, done: [], undone: [] })
  const [rejection, setRejection] = useState<string>()
  const types = useMemo(() => nodeTypes ?? new Map<string, NodeType>(), [nodeTypes])

  const pending = commands.pending(q.pos).filter((p) => p.target === graphId)
  const view = useMemo(() => {
    if (!q.data) return undefined
    return pending.reduce((v: GraphView, p: PendingCommand) => applyOp(v, { command: p.command, args: (p.args ?? {}) as Record<string, unknown> }, types), q.data)
    // pending is a new array each render; its content is what matters
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.data, JSON.stringify(pending.map((p) => p.key)), types])
  const viewRef = useRef(view)
  useEffect(() => {
    viewRef.current = view
  }, [view])

  const run = useCallback(
    (ops: Op[]) => {
      for (const op of ops) commands.send(op.command, graphId, op.args).catch((err: unknown) => setRejection(message(err)))
    },
    [commands, graphId],
  )
  const apply = useCallback(
    (ops: Op[]) => {
      let v = viewRef.current
      if (!v || ops.length === 0) return
      const undo: Op[] = []
      for (const op of ops) {
        undo.unshift(...inverse(v, op))
        v = applyOp(v, op, types)
      }
      setStacks((s) => ({ graphId, done: [...(s.graphId === graphId ? s.done : []), { redo: ops, undo }].slice(-200), undone: [] }))
      run(ops)
    },
    [graphId, run, types],
  )
  const current = useMemo(() => (stacks.graphId === graphId ? stacks : { graphId, done: [], undone: [] }), [stacks, graphId])
  const undo = useCallback(() => {
    const e = current.done.at(-1)
    if (!e) return
    setStacks({ graphId, done: current.done.slice(0, -1), undone: [...current.undone, e] })
    run(e.undo)
  }, [current, graphId, run])
  const redo = useCallback(() => {
    const e = current.undone.at(-1)
    if (!e) return
    setStacks({ graphId, done: [...current.done, e], undone: current.undone.slice(0, -1) })
    run(e.redo)
  }, [current, graphId, run])
  const send = useCallback(
    async (command: string, args: Record<string, unknown>) => {
      try {
        await live.command(command, graphId, args)
      } catch (err) {
        setRejection(message(err))
        throw err
      }
    },
    [graphId],
  )

  return {
    view,
    loading: q.loading,
    error: q.error,
    apply,
    send,
    undo,
    redo,
    canUndo: current.done.length > 0,
    canRedo: current.undone.length > 0,
    rejection,
    clearRejection: () => setRejection(undefined),
  }
}

// A new id for a node this browser adds, so its undo can name it
export function newNodeId(view: GraphView | undefined, prefix = 'n'): string {
  const used = new Set(view?.graph.nodes.map((n) => n.id))
  for (;;) {
    const id = `${prefix}${Math.random().toString(36).slice(2, 7)}`
    if (!used.has(id)) return id
  }
}

// A graph id made from a name, with a suffix so copies do not collide
export function graphIdFrom(name: string): string {
  const base = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
    .slice(0, 40)
  return `${base || 'graph'}-${Math.random().toString(36).slice(2, 6)}`
}

// What the Palette and Variables drag onto the canvas
export const dragType = 'application/x-cognition-node'
export interface DraggedNode {
  type: string
  config?: unknown
  title?: string
}

