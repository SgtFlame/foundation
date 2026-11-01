import { memo, useCallback, useMemo, useRef, useState, type DragEvent, type KeyboardEvent } from 'react'
import {
  Background,
  Controls,
  Handle,
  MiniMap,
  NodeResizer,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type Node,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Box, Stack, Tooltip, Typography, useTheme } from '@mui/material'
import ErrorIcon from '@mui/icons-material/Error'
import WarningAmberIcon from '@mui/icons-material/WarningAmber'
import { formatCost, formatTokens } from '../../components/chat/levels.ts'
import type { DataType, Edge as GraphEdge, GraphNode, NodeType, Pin, Pins, Problem } from '../../services/Cognition.ts'
import { formatDuration } from '../../tabs/devtools/network/format.ts'
import { connects, headerColor, typeColor, useCognition, type NodeStats, type Selection } from './cognitionState.ts'
import { dragType, newNodeId, type DraggedNode, type Op } from './editing.ts'
import Toolbar from './Toolbar.tsx'

const nodeWidth = 250

// Copied nodes, kept here as well as on the clipboard, which the browser
// may not let a page read
let copied: { nodes: GraphNode[]; edges: GraphEdge[] } | undefined

interface PinNodeData extends Record<string, unknown> {
  node: GraphNode
  type?: NodeType
  pins: Pins
  problems: Problem[]
  stats?: NodeStats
  failed: boolean
  types: Map<string, DataType>
  execColor: string
  selection: Selection
  readOnly: boolean
  // The colours of the other people who have it selected
  others: string[]
  conversions: Set<string> // the inputs whose edge converts
  onPin: (nodeId: string, pin: Pin, dir: 'in' | 'out') => void
  onResize?: (width: number, height: number) => void
}

type PinNode = Node<PinNodeData>

// An exec pin is a white arrow, a data pin a circle in its type's colour
function PinHandle({ pin, dir, color, execColor, readOnly }: { pin: Pin; dir: 'in' | 'out'; color: string; execColor: string; readOnly: boolean }) {
  const style = pin.exec
    ? { width: 12, height: 12, border: 'none', borderRadius: 0, background: execColor, clipPath: 'polygon(0 0, 100% 50%, 0 100%)' }
    : { width: 10, height: 10, border: `2px solid ${color}`, background: color }
  return (
    <Handle
      type={dir === 'in' ? 'target' : 'source'}
      position={dir === 'in' ? Position.Left : Position.Right}
      id={pin.id}
      isConnectable={!readOnly}
      style={{ ...style, [dir === 'in' ? 'left' : 'right']: -6 }}
    />
  )
}

function PinLabel({ pin, dir, data }: { pin: Pin; dir: 'in' | 'out'; data: PinNodeData }) {
  const selected = data.selection.nodeId === data.node.id && data.selection.pin?.id === pin.id && data.selection.pin.dir === dir
  const problem = data.problems.find((p) => p.pin === pin.id)
  const color = pin.exec ? data.execColor : typeColor(data.types, pin.type)
  return (
    <Tooltip
      placement={dir === 'in' ? 'left' : 'right'}
      title={
        <>
          <b>{pin.name || pin.id}</b> {pin.exec ? 'exec' : pin.type}
          {pin.optional ? ' (optional)' : ''}
          {dir === 'in' && data.conversions.has(pin.id) ? ' (converted)' : ''}
          {pin.description ? <div>{pin.description}</div> : null}
          {problem ? <div>⚠ {problem.message}</div> : null}
        </>
      }
    >
      <Box
        className="nodrag"
        onClick={(e) => {
          e.stopPropagation()
          data.onPin(data.node.id, pin, dir)
        }}
        sx={{
          position: 'relative',
          px: 1.25,
          py: 0.25,
          fontSize: 12,
          lineHeight: '18px',
          textAlign: dir === 'in' ? 'left' : 'right',
          cursor: 'pointer',
          borderRadius: 0.5,
          bgcolor: selected ? 'action.selected' : undefined,
          color: problem ? 'error.main' : 'text.primary',
          '&:hover': { bgcolor: 'action.hover' },
        }}
      >
        <PinHandle pin={pin} dir={dir} color={color} execColor={data.execColor} readOnly={data.readOnly} />
        {/* the label is cut short, not the row: the handle sits on the row's edge */}
        <Box component="span" sx={{ display: 'block', minHeight: 18, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {pin.exec ? pin.name : pin.name || pin.id}
        </Box>
      </Box>
    </Tooltip>
  )
}

// Other people's selections: a ring per person, in their colour
function othersShadow(others: string[]): string | undefined {
  if (others.length === 0) return undefined
  return others.map((c, i) => `0 0 0 ${3 + i * 3}px ${c}`).join(', ')
}

// A node drawn from its type and pins, with its run badge
const PinsNode = memo(function PinsNode({ data, selected }: NodeProps<PinNode>) {
  const { node, type, pins, problems, stats, failed } = data
  const rows = Math.max(pins.in.length, pins.out.length)
  const errors = problems.filter((p) => p.severity === 'error')
  const ran = !!stats
  return (
    <Box
      sx={{
        width: nodeWidth,
        bgcolor: 'background.paper',
        borderRadius: 1,
        boxShadow: othersShadow(data.others) ?? (selected ? 6 : 2),
        outline: failed ? '2px solid' : selected ? '2px solid' : '1px solid',
        outlineColor: failed ? 'error.main' : selected ? 'primary.main' : 'divider',
      }}
    >
      <Stack
        direction="row"
        sx={{ alignItems: 'center', gap: 0.5, px: 1, py: 0.5, bgcolor: headerColor(type), color: '#fff', borderTopLeftRadius: 4, borderTopRightRadius: 4 }}
      >
        <Typography variant="caption" sx={{ fontWeight: 600, flexGrow: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {node.title || type?.title || node.type}
        </Typography>
        {problems.length > 0 && (
          <Tooltip title={problems.map((p) => p.message).join('\n')}>
            {errors.length > 0 ? <ErrorIcon sx={{ fontSize: 16 }} /> : <WarningAmberIcon sx={{ fontSize: 16 }} />}
          </Tooltip>
        )}
      </Stack>
      <Box sx={{ py: 0.5 }}>
        {Array.from({ length: rows }, (_, i) => (
          <Stack key={i} direction="row">
            <Box sx={{ width: '50%', minWidth: 0 }}>{pins.in[i] && <PinLabel pin={pins.in[i]} dir="in" data={data} />}</Box>
            <Box sx={{ width: '50%', minWidth: 0 }}>{pins.out[i] && <PinLabel pin={pins.out[i]} dir="out" data={data} />}</Box>
          </Stack>
        ))}
      </Box>
      {ran && (
        <Tooltip title={stats.error ?? ''}>
          <Stack
            direction="row"
            sx={{
              gap: 1,
              px: 1,
              py: 0.25,
              borderTop: 1,
              borderColor: 'divider',
              fontSize: 11,
              color: failed ? 'error.main' : 'text.secondary',
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            <span>×{stats.count}</span>
            <span>{formatDuration(stats.durationMs)}</span>
            {stats.inputTokens + stats.outputTokens > 0 && (
              <span>
                {formatTokens(stats.inputTokens)}→{formatTokens(stats.outputTokens)}
              </span>
            )}
            {stats.costMicros > 0 && <span>{formatCost(stats.costMicros, stats.currency)}</span>}
            {failed && <span>failed</span>}
          </Stack>
        </Tooltip>
      )}
    </Box>
  )
})

// A reroute: one dot passing a value on
const RerouteNode = memo(function RerouteNode({ data, selected }: NodeProps<PinNode>) {
  const inPin = data.pins.in[0]
  const outPin = data.pins.out[0]
  return (
    <Box sx={{ width: 16, height: 16, borderRadius: '50%', bgcolor: selected ? 'primary.main' : 'text.secondary', boxShadow: othersShadow(data.others), position: 'relative' }}>
      {inPin && <PinHandle pin={inPin} dir="in" color="transparent" execColor={data.execColor} readOnly={data.readOnly} />}
      {outPin && <PinHandle pin={outPin} dir="out" color="transparent" execColor={data.execColor} readOnly={data.readOnly} />}
    </Box>
  )
})

interface CommentConfig {
  text?: string
  color?: string
  width?: number
  height?: number
}

// A comment frame drawn behind the nodes it groups
const CommentNode = memo(function CommentNode({ data, selected }: NodeProps<PinNode>) {
  const c = (data.node.config ?? {}) as CommentConfig
  const color = c.color || '#90a4ae'
  return (
    <Box sx={{ width: '100%', height: '100%', border: 2, borderColor: color, borderRadius: 1, bgcolor: `${color}22`, boxShadow: othersShadow(data.others) }}>
      <NodeResizer isVisible={selected && !data.readOnly} minWidth={160} minHeight={80} onResizeEnd={(_, p) => data.onResize?.(p.width, p.height)} />
      <Typography variant="subtitle2" sx={{ px: 1, py: 0.5, bgcolor: color, color: '#fff' }}>
        {c.text || data.node.title || 'Comment'}
      </Typography>
    </Box>
  )
})

const flowNodeTypes = { pins: PinsNode, reroute: RerouteNode, comment: CommentNode }

const edgeKey = (e: GraphEdge) => `${e.from.node}.${e.from.pin}>${e.to.node}.${e.to.pin}`

function Canvas() {
  const state = useCognition()
  const theme = useTheme()
  const flow = useReactFlow()
  const { graph, pins, problems, nodeTypes: types, types: dataTypes, stats, trace, selection, select, readOnly, editing, view, others, conversions } = state
  const execColor = theme.palette.mode === 'dark' ? '#eceff1' : '#455a64'
  // The canvas is controlled, so the sizes React Flow measures come back as
  // changes; the minimap and fitView need them. Positions stay here while
  // a node is dragged, and are sent when it is dropped
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({})
  const [dragging, setDragging] = useState<Record<string, { x: number; y: number }>>({})
  const [selectedEdges, setSelectedEdges] = useState<string[]>([])
  const wrapper = useRef<HTMLDivElement>(null)
  const { selectedNodes, setSelectedNodes } = state

  const othersOn = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const o of others) {
      for (const id of (o.state?.selected as string[] | undefined) ?? []) m.set(id, [...(m.get(id) ?? []), o.color])
    }
    return m
  }, [others])
  const converted = useMemo(() => {
    const m = new Map<string, Set<string>>()
    for (const c of view.conversions ?? []) m.set(c.node, (m.get(c.node) ?? new Set()).add(c.pin))
    return m
  }, [view.conversions])

  const apply = useCallback((ops: Op[]) => !readOnly && editing.apply(ops), [readOnly, editing])

  const onNodesChange = (changes: NodeChange<PinNode>[]) => {
    const sizes = changes.flatMap((c) => (c.type === 'dimensions' && c.dimensions ? [[c.id, c.dimensions] as const] : []))
    if (sizes.length) setMeasured((m) => ({ ...m, ...Object.fromEntries(sizes) }))
    const picks = changes.filter((c) => c.type === 'select')
    if (picks.length) {
      const set = new Set(selectedNodes)
      for (const c of picks) {
        if (c.type !== 'select') continue
        if (c.selected) set.add(c.id)
        else set.delete(c.id)
      }
      setSelectedNodes([...set])
    }
    const moves = changes.filter((c) => c.type === 'position')
    if (moves.length && !readOnly) {
      const next = { ...dragging }
      const dropped: { node: string; position: { x: number; y: number } }[] = []
      for (const c of moves) {
        if (c.type !== 'position') continue
        if (c.position) next[c.id] = c.position
        if (c.dragging === false && next[c.id]) {
          dropped.push({ node: c.id, position: next[c.id] })
          delete next[c.id]
        }
      }
      setDragging(next)
      if (dropped.length) apply([{ command: 'bp.moveNodes', args: { moves: dropped } }])
    }
  }
  const onEdgesChange = (changes: EdgeChange[]) => {
    const picks = changes.filter((c) => c.type === 'select')
    if (!picks.length) return
    const set = new Set(selectedEdges)
    for (const c of picks) {
      if (c.type !== 'select') continue
      if (c.selected) set.add(c.id)
      else set.delete(c.id)
    }
    setSelectedEdges([...set])
  }

  const nodes = useMemo<PinNode[]>(
    () =>
      graph.nodes.map((n) => {
        const kind = n.type === 'note.comment' ? 'comment' : n.type === 'flow.reroute' ? 'reroute' : 'pins'
        const c = (n.config ?? {}) as CommentConfig
        return {
          id: n.id,
          type: kind,
          position: dragging[n.id] ?? n.position,
          measured: measured[n.id],
          selected: selectedNodes.includes(n.id),
          zIndex: kind === 'comment' ? -1 : undefined,
          style: kind === 'comment' ? { width: c.width ?? 360, height: c.height ?? 220 } : undefined,
          draggable: !readOnly,
          data: {
            node: n,
            type: types.get(n.type),
            pins: { in: pins[n.id]?.in ?? [], out: pins[n.id]?.out ?? [] },
            problems: problems.filter((p) => p.nodeId === n.id),
            stats: stats.get(n.id),
            failed: trace?.run?.failedNode === n.id,
            types: dataTypes,
            execColor,
            selection,
            readOnly,
            others: othersOn.get(n.id) ?? [],
            conversions: converted.get(n.id) ?? new Set(),
            onPin: (nodeId, pin, dir) => select({ nodeId, pin: { id: pin.id, dir } }),
            onResize: (width, height) =>
              apply([{ command: 'bp.setConfig', args: { node: n.id, config: { ...c, width: Math.round(width), height: Math.round(height) } } }]),
          },
        }
      }),
    [graph, types, pins, problems, stats, trace, dataTypes, execColor, selection, select, measured, dragging, selectedNodes, readOnly, othersOn, converted, apply],
  )

  const edges = useMemo<Edge[]>(() => {
    // An exec edge ran when its node left by its pin; a data edge when both ends ran
    const taken = new Set((trace?.steps ?? []).filter((s) => s.execPin).map((s) => `${s.nodeId}.${s.execPin}`))
    return graph.edges.map((e) => {
      const out = pins[e.from.node]?.out.find((p) => p.id === e.from.pin)
      const exec = !!out?.exec
      const ran = exec ? taken.has(`${e.from.node}.${e.from.pin}`) : stats.has(e.from.node) && stats.has(e.to.node)
      const color = exec ? execColor : typeColor(dataTypes, out?.type)
      const conv = view.conversions?.find((c) => c.node === e.to.node && c.pin === e.to.pin)
      const id = edgeKey(e)
      return {
        id,
        source: e.from.node,
        sourceHandle: e.from.pin,
        target: e.to.node,
        targetHandle: e.to.pin,
        animated: ran,
        selected: selectedEdges.includes(id),
        label: conv ? `${conv.from}→${conv.to}` : undefined,
        labelStyle: { fontSize: 10 },
        style: { stroke: color, strokeWidth: exec ? 3 : 2, opacity: trace?.run && !ran ? 0.35 : 1 },
      }
    })
  }, [graph, pins, trace, stats, dataTypes, execColor, view.conversions, selectedEdges])

  // Exec joins exec and data joins data of a type that matches or converts
  const isValidConnection = (c: Connection | Edge) => {
    if (!c.source || !c.target || c.source === c.target) return false
    const out = pins[c.source]?.out.find((p) => p.id === c.sourceHandle)
    const inp = pins[c.target]?.in.find((p) => p.id === c.targetHandle)
    if (!out || !inp || !!out.exec !== !!inp.exec) return false
    return !!out.exec || connects(conversions, out.type, inp.type)
  }
  const onConnect = (c: Connection) => {
    if (!c.source || !c.target || !c.sourceHandle || !c.targetHandle) return
    apply([{ command: 'bp.connect', args: { from: { node: c.source, pin: c.sourceHandle }, to: { node: c.target, pin: c.targetHandle } } }])
  }

  const removeSelection = () => {
    const removed = graph.edges.filter((e) => selectedEdges.includes(edgeKey(e)))
    if (!selectedNodes.length && !removed.length) return
    apply([{ command: 'bp.remove', args: { nodes: selectedNodes, edges: removed } }])
    setSelectedNodes([])
    setSelectedEdges([])
    select({})
  }
  const copy = () => {
    const ids = new Set(selectedNodes)
    if (!ids.size) return
    copied = { nodes: graph.nodes.filter((n) => ids.has(n.id)), edges: graph.edges.filter((e) => ids.has(e.from.node) && ids.has(e.to.node)) }
    void navigator.clipboard?.writeText(JSON.stringify({ cognitionFragment: copied })).catch(() => undefined)
  }
  const paste = async () => {
    let fragment = copied
    try {
      const text = await navigator.clipboard?.readText()
      const parsed = text ? (JSON.parse(text) as { cognitionFragment?: typeof copied }) : undefined
      if (parsed?.cognitionFragment) fragment = parsed.cognitionFragment
    } catch {
      // the clipboard holds something else, or cannot be read: use our own copy
    }
    if (!fragment?.nodes.length) return
    const ids = new Map(fragment.nodes.map((n) => [n.id, newNodeId(view, 'p')]))
    const nodes = fragment.nodes.map((n) => ({ ...n, id: ids.get(n.id)!, position: { x: n.position.x + 40, y: n.position.y + 40 } }))
    const pasted = fragment.edges.map((e) => ({ from: { ...e.from, node: ids.get(e.from.node)! }, to: { ...e.to, node: ids.get(e.to.node)! } }))
    apply([{ command: 'bp.paste', args: { nodes, edges: pasted } }])
    setSelectedNodes(nodes.map((n) => n.id))
  }

  const onKeyDown = (e: KeyboardEvent) => {
    const target = e.target as HTMLElement
    if (target.closest('input, textarea, [contenteditable="true"]')) return
    const mod = e.ctrlKey || e.metaKey
    if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault()
      removeSelection()
    } else if (mod && e.key.toLowerCase() === 'z') {
      e.preventDefault()
      if (e.shiftKey) editing.redo()
      else editing.undo()
    } else if (mod && e.key.toLowerCase() === 'y') {
      e.preventDefault()
      editing.redo()
    } else if (mod && e.key.toLowerCase() === 'c') {
      copy()
    } else if (mod && e.key.toLowerCase() === 'v' && !readOnly) {
      e.preventDefault()
      void paste()
    }
  }

  const onDrop = (e: DragEvent) => {
    const raw = e.dataTransfer.getData(dragType)
    if (!raw || readOnly) return
    e.preventDefault()
    const d = JSON.parse(raw) as DraggedNode
    const position = flow.screenToFlowPosition({ x: e.clientX, y: e.clientY })
    const id = newNodeId(view)
    apply([{ command: 'bp.addNode', args: { type: d.type, id, config: d.config, title: d.title, position: { x: Math.round(position.x), y: Math.round(position.y) } } }])
    setSelectedNodes([id])
    select({ nodeId: id })
  }

  return (
    <Stack sx={{ width: '100%', height: '100%' }}>
      <Toolbar onDelete={removeSelection} canDelete={selectedNodes.length + selectedEdges.length > 0} />
      <Box
        ref={wrapper}
        tabIndex={0}
        onKeyDown={onKeyDown}
        onDragOver={(e) => {
          if (e.dataTransfer.types.includes(dragType)) e.preventDefault()
        }}
        onDrop={onDrop}
        sx={{ flexGrow: 1, minHeight: 0, outline: 'none' }}
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={flowNodeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          isValidConnection={isValidConnection}
          colorMode={theme.palette.mode}
          nodesDraggable={!readOnly}
          nodesConnectable={!readOnly}
          deleteKeyCode={null}
          selectionKeyCode="Shift"
          multiSelectionKeyCode={['Meta', 'Control']}
          onNodeClick={(_, n) => {
            select({ nodeId: n.id })
            wrapper.current?.focus()
          }}
          onPaneClick={() => {
            select({})
            setSelectedNodes([])
            setSelectedEdges([])
            wrapper.current?.focus()
          }}
          fitView
          minZoom={0.2}
          proOptions={{ hideAttribution: true }}
        >
          <Background />
          <Controls showInteractive={false} />
          <MiniMap pannable zoomable nodeColor={(n) => headerColor(types.get((n.data as PinNodeData).node.type))} />
        </ReactFlow>
      </Box>
    </Stack>
  )
}

// The Logic Editor: the graph's draft on an editable canvas, with the
// selected run's overlays and the other authors' selections
function GraphCanvas() {
  return (
    <Box sx={{ width: '100%', height: '100%' }}>
      <ReactFlowProvider>
        <Canvas />
      </ReactFlowProvider>
    </Box>
  )
}

export default GraphCanvas
