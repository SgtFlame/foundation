// graph.canvas: a graph editor on React Flow, for graphs an application
// keeps in its records (not the Cognition canvas). Nodes and edges come
// from props; positions live here while dragging and are sent on drag end
// (move). Connecting, deleting, selecting, dropping from the palette and
// double-clicking are events. A child keyed by a node's id is that node's
// body. Other users' selections are outlined in their colour, from the
// tab's presence.

import {
  applyNodeChanges,
  Background,
  Controls,
  Handle,
  Position as Side,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Box, Paper, Stack, Typography, useTheme } from '@mui/material'
import { createContext, useContext, useEffect, useMemo, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { presentName } from '../../../live/PresenceAvatars.tsx'
import type { RendererProps } from '../../points.ts'
import { useUiHost } from '../../UiHostContext.ts'

interface GraphNodeProp {
  id: string
  position?: { x: number; y: number }
  label?: string
  type?: string
  ports?: { in?: string[]; out?: string[] }
}

interface GraphEdgeProp {
  id: string
  source: string
  target: string
  sourceHandle?: string
  targetHandle?: string
}

interface NodeData extends Record<string, unknown> {
  label: string
  ports: { in?: string[]; out?: string[] }
  outline?: string // another user's colour, when they have it selected
  outlineBy?: string
}

// Node bodies are the canvas's children, rendered by key
const BodyContext = createContext<(key: string) => ReactNode>(() => null)

function UiGraphNode({ id, data, selected }: NodeProps<Node<NodeData>>) {
  const renderBody = useContext(BodyContext)
  const theme = useTheme()
  const ins = data.ports.in ?? []
  const outs = data.ports.out ?? []
  const body = renderBody(id)
  return (
    <Paper
      variant="outlined"
      title={data.outlineBy ? `Selected by ${data.outlineBy}` : undefined}
      sx={{
        minWidth: 120,
        px: 1.5,
        py: 1,
        borderWidth: 2,
        borderColor: selected ? theme.palette.primary.main : (data.outline ?? theme.palette.divider),
        boxShadow: data.outline ? `0 0 0 2px ${data.outline}55` : undefined,
      }}
    >
      {ins.map((port, i) => (
        <Handle key={port} type="target" id={port} position={Side.Left} style={{ top: `${((i + 1) * 100) / (ins.length + 1)}%` }} />
      ))}
      <Typography variant="body2" sx={{ fontWeight: 500 }}>
        {data.label}
      </Typography>
      {body && <Box sx={{ mt: 1 }} className="nodrag">{body}</Box>}
      {outs.map((port, i) => (
        <Handle key={port} type="source" id={port} position={Side.Right} style={{ top: `${((i + 1) * 100) / (outs.length + 1)}%` }} />
      ))}
    </Paper>
  )
}

const nodeTypes = { ui: UiGraphNode }

function Canvas(p: RendererProps) {
  const theme = useTheme()
  const host = useUiHost()
  const flow = useReactFlow()
  const propNodes = useMemo(() => (p.props.nodes as GraphNodeProp[]) ?? [], [p.props.nodes])
  const propEdges = useMemo(() => (p.props.edges as GraphEdgeProp[]) ?? [], [p.props.edges])
  const palette = (p.props.palette as { id: string; label?: string }[]) ?? []

  // Who else has which node selected
  const outlines = useMemo(() => {
    const out = new Map<string, { color: string; name: string }>()
    for (const u of host.others) {
      const sel = (u.state?.selected as string[] | undefined) ?? []
      for (const id of sel) if (!out.has(id)) out.set(id, { color: u.color, name: presentName(u) })
    }
    return out
  }, [host.others])

  const [nodes, setNodes] = useState<Node<NodeData>[]>([])
  const dragging = useRef(new Set<string>())
  // The server's nodes, keeping positions being dragged and the selection
  useEffect(() => {
    setNodes((current) => {
      const byId = new Map(current.map((n) => [n.id, n]))
      return propNodes.map((n) => {
        const old = byId.get(n.id)
        const o = outlines.get(n.id)
        return {
          id: n.id,
          type: 'ui',
          position: dragging.current.has(n.id) && old ? old.position : (n.position ?? { x: 0, y: 0 }),
          selected: old?.selected,
          data: { label: n.label ?? n.id, ports: n.ports ?? { in: ['in'], out: ['out'] }, outline: o?.color, outlineBy: o?.name },
        }
      })
    })
  }, [propNodes, outlines])

  const edges: Edge[] = useMemo(
    () => propEdges.map((e) => ({ id: e.id, source: e.source, target: e.target, sourceHandle: e.sourceHandle, targetHandle: e.targetHandle })),
    [propEdges],
  )

  const onNodesChange = (changes: NodeChange<Node<NodeData>>[]) => {
    for (const c of changes) {
      if (c.type === 'position') {
        if (c.dragging) dragging.current.add(c.id)
      }
    }
    setNodes((ns) => applyNodeChanges(changes, ns))
  }

  const onDrop = (e: DragEvent) => {
    const item = e.dataTransfer.getData('application/x-ui-palette')
    if (!item) return
    e.preventDefault()
    p.emit('drop', { item, position: flow.screenToFlowPosition({ x: e.clientX, y: e.clientY }) })
  }

  return (
    <Stack direction="row" sx={{ height: (p.props.height as number) ?? '100%', minHeight: 240 }}>
      {palette.length > 0 && (
        <Stack spacing={0.5} sx={{ p: 1, borderRight: 1, borderColor: 'divider', width: 140 }}>
          {palette.map((item) => (
            <Paper key={item.id} variant="outlined" draggable sx={{ px: 1, py: 0.5, cursor: 'grab' }}
              onDragStart={(e) => e.dataTransfer.setData('application/x-ui-palette', item.id)}>
              <Typography variant="body2">{item.label ?? item.id}</Typography>
            </Paper>
          ))}
        </Stack>
      )}
      <Box sx={{ flex: 1, minWidth: 0 }} onDragOver={(e) => e.preventDefault()} onDrop={onDrop}>
        <BodyContext.Provider value={p.renderChild}>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            colorMode={theme.palette.mode}
            fitView
            onNodesChange={onNodesChange}
            onNodeDragStop={(_, node, dragged) => {
              for (const n of dragged.length ? dragged : [node]) {
                dragging.current.delete(n.id)
                p.emit('move', { id: n.id, position: { x: Math.round(n.position.x), y: Math.round(n.position.y) } })
              }
            }}
            onConnect={(c) => p.emit('connect', { source: c.source, sourceHandle: c.sourceHandle ?? undefined, target: c.target, targetHandle: c.targetHandle ?? undefined })}
            onEdgesDelete={(deleted) => {
              if (p.handles('delete')) p.emit('delete', { nodes: [], edges: deleted.map((e) => e.id) })
              else for (const e of deleted) p.emit('disconnect', { id: e.id })
            }}
            onNodesDelete={(deleted) => p.emit('delete', { nodes: deleted.map((n) => n.id), edges: [] })}
            onSelectionChange={({ nodes: sel, edges: selEdges }) => {
              const ids = sel.map((n) => n.id)
              host.setPresence({ selected: ids })
              p.emit('select', { nodes: ids, edges: selEdges.map((e) => e.id) })
            }}
            onNodeDoubleClick={(_, node) => p.emit('open', { id: node.id })}
            deleteKeyCode={['Backspace', 'Delete']}
          >
            <Background />
            <Controls showInteractive={false} />
          </ReactFlow>
        </BodyContext.Provider>
      </Box>
    </Stack>
  )
}

function GraphCanvas(p: RendererProps) {
  return (
    <ReactFlowProvider>
      <Canvas {...p} />
    </ReactFlowProvider>
  )
}

export default GraphCanvas
