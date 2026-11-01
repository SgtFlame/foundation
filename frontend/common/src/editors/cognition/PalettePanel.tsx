import { useMemo, useState } from 'react'
import { Box, InputAdornment, List, ListItemButton, ListItemText, TextField, Tooltip, Typography } from '@mui/material'
import SearchIcon from '@mui/icons-material/Search'
import { allows, type NodeType } from '../../services/Cognition.ts'
import { headerColor, useCognition } from './cognitionState.ts'
import { dragType, newNodeId, type DraggedNode } from './editing.ts'

// The Palette: the node types the graph's kind allows, by category, with
// search. Drag one onto the canvas, or double-click it to add it beside the
// others.
function PalettePanel() {
  const state = useCognition()
  const { nodeTypes, kind, graph, readOnly, editing, view } = state
  const [search, setSearch] = useState('')
  const groups = useMemo(() => {
    const q = search.trim().toLowerCase()
    const present = new Set(graph.nodes.map((n) => n.type))
    const byCategory = new Map<string, NodeType[]>()
    for (const t of nodeTypes.values()) {
      if (!allows(kind, t) || (t.event && present.has(t.id))) continue
      if (q && !`${t.title} ${t.category} ${t.description} ${t.id}`.toLowerCase().includes(q)) continue
      byCategory.set(t.category, [...(byCategory.get(t.category) ?? []), t])
    }
    return [...byCategory.entries()].sort(([a], [b]) => (a === 'Events' ? -1 : b === 'Events' ? 1 : a.localeCompare(b)))
  }, [nodeTypes, kind, graph.nodes, search])

  const add = (t: NodeType) => {
    if (readOnly) return
    const id = newNodeId(view)
    editing.apply([{ command: 'bp.addNode', args: { type: t.id, id } }])
    state.setSelectedNodes([id])
    state.select({ nodeId: id })
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <TextField
        size="small"
        placeholder="Search nodes"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        sx={{ m: 1 }}
        slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment> } }}
      />
      {kind && (
        <Typography variant="caption" sx={{ px: 1.5, color: 'text.secondary' }}>
          For a {kind.title} graph
        </Typography>
      )}
      <Box sx={{ flexGrow: 1, overflow: 'auto' }}>
        {groups.map(([category, list]) => (
          <List
            key={category}
            dense
            disablePadding
            subheader={
              <Typography variant="caption" sx={{ display: 'block', px: 1.5, pt: 1, color: headerColor(list[0]), fontWeight: 600 }}>
                {category}
              </Typography>
            }
          >
            {list.map((t) => (
              <Tooltip key={t.id} placement="right" title={<>{t.description}{t.example ? <><br /><i>{t.example}</i></> : null}</>}>
                <ListItemButton
                  draggable={!readOnly}
                  onDragStart={(e) => {
                    e.dataTransfer.setData(dragType, JSON.stringify({ type: t.id } satisfies DraggedNode))
                    e.dataTransfer.effectAllowed = 'copy'
                  }}
                  onDoubleClick={() => add(t)}
                  sx={{ py: 0.25, borderLeft: 3, borderColor: headerColor(t), ml: 1, cursor: readOnly ? 'default' : 'grab' }}
                >
                  <ListItemText primary={t.title} slotProps={{ primary: { variant: 'body2' } }} />
                </ListItemButton>
              </Tooltip>
            ))}
          </List>
        ))}
      </Box>
    </Box>
  )
}

export default PalettePanel
