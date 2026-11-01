import { useState } from 'react'
import { Box, Button, Chip, IconButton, MenuItem, Stack, TextField, Tooltip, Typography } from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import type { Variable } from '../../services/Cognition.ts'
import { typeColor, useCognition } from './cognitionState.ts'
import { dragType, type DraggedNode } from './editing.ts'

function parseDefault(text: string): unknown {
  if (text.trim() === '') return undefined
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

// A Get or Set chip that drags a variable node onto the canvas
function VariableChip({ v, set }: { v: Variable; set?: boolean }) {
  const { readOnly } = useCognition()
  const node: DraggedNode = { type: set ? 'data.setVariable' : 'data.getVariable', config: { variable: v.id, type: v.type }, title: `${set ? 'Set' : 'Get'} ${v.name}` }
  return (
    <Tooltip title={`Drag onto the canvas: ${node.title}`}>
      <Chip
        size="small"
        label={set ? 'Set' : 'Get'}
        draggable={!readOnly}
        onDragStart={(e) => e.dataTransfer.setData(dragType, JSON.stringify(node))}
        sx={{ cursor: 'grab' }}
      />
    </Tooltip>
  )
}

// The graph's variables: per-run state, read by Get variable and written by
// Set variable
function VariablesPanel() {
  const { graph, types, readOnly, editing } = useCognition()
  const [name, setName] = useState('')
  const [type, setType] = useState('string')
  const [value, setValue] = useState('')
  const vars = graph.variables ?? []
  const add = () => {
    const id = name.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '_')
    editing.apply([{ command: 'bp.addVariable', args: { id, name: name.trim(), type, default: parseDefault(value) } }])
    setName('')
    setValue('')
  }
  return (
    <Box sx={{ height: '100%', overflow: 'auto', p: 1 }}>
      {vars.length === 0 && (
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>
          No variables. A variable holds a value for one run, in place of a long wire.
        </Typography>
      )}
      {vars.map((v) => (
        <Stack key={v.id} direction="row" sx={{ alignItems: 'center', gap: 0.5, py: 0.25 }}>
          <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: typeColor(types, v.type), flexShrink: 0 }} />
          <Typography variant="body2" sx={{ flexGrow: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }} title={v.description}>
            {v.name} <Typography component="span" variant="caption" color="text.secondary">{v.type}{v.default !== undefined ? ` = ${JSON.stringify(v.default)}` : ''}</Typography>
          </Typography>
          <VariableChip v={v} />
          <VariableChip v={v} set />
          <IconButton size="small" aria-label={`Remove ${v.name}`} disabled={readOnly} onClick={() => editing.apply([{ command: 'bp.removeVariable', args: { id: v.id } }])}>
            <DeleteIcon fontSize="small" />
          </IconButton>
        </Stack>
      ))}
      {!readOnly && (
        <Stack sx={{ gap: 1, mt: 1.5 }}>
          <TextField size="small" label="New variable" value={name} onChange={(e) => setName(e.target.value)} />
          <Stack direction="row" sx={{ gap: 1 }}>
            <TextField select size="small" label="Type" value={type} onChange={(e) => setType(e.target.value)} sx={{ flex: 1 }}>
              {[...types.keys()].map((t) => (
                <MenuItem key={t} value={t}>
                  {t}
                </MenuItem>
              ))}
            </TextField>
            <TextField size="small" label="Default" value={value} onChange={(e) => setValue(e.target.value)} sx={{ flex: 1 }} />
          </Stack>
          <Button size="small" variant="outlined" onClick={add} disabled={!name.trim()}>
            Add variable
          </Button>
        </Stack>
      )}
    </Box>
  )
}

export default VariablesPanel
