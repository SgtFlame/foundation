// chat.sources (forge 15b): a list of cited segments drawn as a chat
// round's Sources row is, for an agent run's or a draft's citations. A
// click opens the passage at its lines, or the Chronicle entry, as a
// round's citation does (components/chat/openSegment.ts). A memory entry
// opens only for its owner: anyone else sees its title, without the link
// or its text.

import { useState } from 'react'
import { Box, Button, Collapse, Link, Stack, Typography } from '@mui/material'
import { openSegment, openable } from '../../../components/chat/openSegment.ts'
import { useSession } from '../../../session/session.ts'
import { useUiHost } from '../../UiHostContext.ts'
import type { RendererProps } from '../../points.ts'

interface Source {
  number: number
  sourceId?: string
  key?: string
  title: string
  snippet?: string
  location: { volumeId?: string; path?: string; startLine?: number; endLine?: number; entryId?: string; owner?: string }
}

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))
const num = (v: unknown) => (typeof v === 'number' ? v : Number(v) || 0)

function sourcesOf(v: unknown): Source[] {
  if (!Array.isArray(v)) return []
  return v
    .filter((s): s is Record<string, unknown> => !!s && typeof s === 'object')
    .map((s) => {
      const loc = (s.location && typeof s.location === 'object' ? s.location : {}) as Record<string, unknown>
      return {
        number: num(s.number),
        sourceId: str(s.sourceId),
        key: str(s.key),
        title: str(s.title) || str(loc.path) || 'Untitled',
        snippet: str(s.snippet),
        location: {
          volumeId: str(loc.volumeId) || undefined,
          path: str(loc.path) || undefined,
          startLine: num(loc.startLine) || undefined,
          endLine: num(loc.endLine) || undefined,
          entryId: str(loc.entryId) && str(loc.entryId) !== '0' ? str(loc.entryId) : undefined,
          owner: str(loc.owner) || undefined,
        },
      }
    })
    .sort((a, b) => a.number - b.number)
}

function lines(s: Source): string {
  const { startLine, endLine } = s.location
  if (!startLine) return ''
  return endLine && endLine !== startLine ? `L${startLine}–L${endLine}` : `L${startLine}`
}

export function ChatSources({ props, emit, handles }: RendererProps) {
  const sources = sourcesOf(props.segments)
  const { registry } = useUiHost()
  const me = useSession().account?.user.id
  const collapsible = props.collapsible === true
  const [open, setOpen] = useState(!collapsible)
  if (sources.length === 0) {
    return props.empty ? (
      <Typography variant="caption" color="text.secondary">
        {str(props.empty)}
      </Typography>
    ) : null
  }
  // Another person's memory entry: its title only
  const hidden = (s: Source) => !!s.location.entryId && !!s.location.owner && s.location.owner !== me
  const list = (
    <Stack spacing={1} sx={{ mt: collapsible ? 1 : 0 }}>
      {sources.map((s) => (
        <Box key={`${s.number}-${s.key}`} sx={{ pl: 1, borderLeft: 2, borderColor: 'divider' }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline' }}>
            <Typography variant="caption" sx={{ fontWeight: 600, color: 'primary.main' }}>
              [{s.number}]
            </Typography>
            {registry && openable(s) && !hidden(s) ? (
              <Link
                component="button"
                variant="body2"
                sx={{ textAlign: 'left' }}
                onClick={() => {
                  openSegment(registry, s)
                  if (handles('open')) emit('open', { number: s.number, sourceId: s.sourceId, key: s.key })
                }}
              >
                {s.title}
              </Link>
            ) : (
              <Typography variant="body2">{s.title}</Typography>
            )}
            <Typography variant="caption" color="text.secondary">
              {s.location.entryId ? (hidden(s) ? 'Someone’s memory' : 'Memory') : lines(s)}
            </Typography>
          </Stack>
          {s.snippet && !hidden(s) && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {s.snippet}
            </Typography>
          )}
        </Box>
      ))}
    </Stack>
  )
  if (!collapsible) {
    return (
      <Box>
        {props.title !== '' && (
          <Typography variant="overline" color="text.secondary">
            {str(props.title) || 'Sources'}
          </Typography>
        )}
        {list}
      </Box>
    )
  }
  return (
    <Box>
      <Button size="small" variant={open ? 'contained' : 'text'} onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        {str(props.title) || 'Sources'} ({sources.length})
      </Button>
      <Collapse in={open} unmountOnExit>
        {list}
      </Collapse>
    </Box>
  )
}
