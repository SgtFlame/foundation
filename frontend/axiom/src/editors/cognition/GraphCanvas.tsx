import { memo, useMemo, useState } from 'react'
import {
  Background,
  Controls,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  type Edge,
  type Node,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Box, Stack, Tooltip, Typography, useTheme } from '@mui/material'
import ErrorIcon from '@mui/icons-material/Error'
import WarningAmberIcon from '@mui/icons-material/WarningAmber'
import { formatCost, formatTokens } from '../../components/chat/levels.ts'
import type { DataType, GraphNode, NodeType, Pin, Pins, Problem } from '../../services/Cognition.ts'
import { formatDuration } from '../../tabs/devtools/network/format.ts'
import { headerColor, typeColor, useCognition, type NodeStats, type Selection } from './cognitionState.ts'

const nodeWidth = 250

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
  onPin: (nodeId: string, pin: Pin, dir: 'in' | 'out') => void
}

type PinNode = Node<PinNodeData, 'pins'>

// An exec pin is a white arrow, a data pin a circle in its type's colour
function PinHandle({ pin, dir, color, execColor }: { pin: Pin; dir: 'in' | 'out'; color: string; execColor: string }) {
  const style = pin.exec
    ? { width: 12, height: 12, border: 'none', borderRadius: 0, background: execColor, clipPath: 'polygon(0 0, 100% 50%, 0 100%)' }
    : { width: 10, height: 10, border: `2px solid ${color}`, background: color }
  return (
    <Handle
      type={dir === 'in' ? 'target' : 'source'}
      position={dir === 'in' ? Position.Left : Position.Right}
      id={pin.id}
      isConnectable={false}
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
        <PinHandle pin={pin} dir={dir} color={color} execColor={data.execColor} />
        {/* the label is cut short, not the row: the handle sits on the row's edge */}
        <Box component="span" sx={{ display: 'block', minHeight: 18, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {pin.exec ? pin.name : pin.name || pin.id}
        </Box>
      </Box>
    </Tooltip>
  )
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
        boxShadow: selected ? 6 : 2,
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

const nodeTypes = { pins: PinsNode }

function Canvas() {
  const state = useCognition()
  const theme = useTheme()
  const { graph, pins, problems, nodeTypes: types, types: dataTypes, stats, trace, selection, select } = state
  const execColor = theme.palette.mode === 'dark' ? '#eceff1' : '#455a64'
  // The canvas is controlled, so the sizes React Flow measures come back as
  // changes; the minimap and fitView need them
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({})
  const onNodesChange = (changes: NodeChange<PinNode>[]) => {
    const sizes = changes.flatMap((c) => (c.type === 'dimensions' && c.dimensions ? [[c.id, c.dimensions] as const] : []))
    if (sizes.length) setMeasured((m) => ({ ...m, ...Object.fromEntries(sizes) }))
  }

  const nodes = useMemo<PinNode[]>(
    () =>
      graph.nodes.map((n) => ({
        id: n.id,
        type: 'pins',
        position: n.position,
        measured: measured[n.id],
        selected: selection.nodeId === n.id,
        data: {
          node: n,
          type: types.get(n.type),
          pins: pins[n.id] ?? { in: [], out: [] },
          problems: problems.filter((p) => p.nodeId === n.id),
          stats: stats.get(n.id),
          failed: trace?.run?.failedNode === n.id,
          types: dataTypes,
          execColor,
          selection,
          onPin: (nodeId, pin, dir) => select({ nodeId, pin: { id: pin.id, dir } }),
        },
      })),
    [graph, types, pins, problems, stats, trace, dataTypes, execColor, selection, select, measured],
  )

  const edges = useMemo<Edge[]>(() => {
    // An exec edge ran when its node left by its pin; a data edge when both ends ran
    const taken = new Set((trace?.steps ?? []).filter((s) => s.execPin).map((s) => `${s.nodeId}.${s.execPin}`))
    return graph.edges.map((e, i) => {
      const out = pins[e.from.node]?.out.find((p) => p.id === e.from.pin)
      const exec = !!out?.exec
      const ran = exec ? taken.has(`${e.from.node}.${e.from.pin}`) : stats.has(e.from.node) && stats.has(e.to.node)
      const color = exec ? execColor : typeColor(dataTypes, out?.type)
      return {
        id: `e${i}`,
        source: e.from.node,
        sourceHandle: e.from.pin,
        target: e.to.node,
        targetHandle: e.to.pin,
        animated: ran,
        style: { stroke: color, strokeWidth: exec ? 3 : 2, opacity: trace?.run && !ran ? 0.35 : 1 },
      }
    })
  }, [graph, pins, trace, stats, dataTypes, execColor])

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodesChange={onNodesChange}
      colorMode={theme.palette.mode}
      nodesDraggable={false}
      nodesConnectable={false}
      edgesFocusable={false}
      onNodeClick={(_, n) => select({ nodeId: n.id })}
      onPaneClick={() => select({})}
      fitView
      minZoom={0.2}
      proOptions={{ hideAttribution: true }}
    >
      <Background />
      <Controls showInteractive={false} />
      <MiniMap pannable zoomable nodeColor={(n) => headerColor(types.get((n.data as PinNodeData).node.type))} />
    </ReactFlow>
  )
}

// The Logic Editor: the graph on a read-only canvas, with the selected
// run's overlays (slice 7; editing comes with shared Yjs drafts)
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
