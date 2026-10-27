import { useState } from 'react'
import { Box, Button, Collapse, Link, Stack, Table, TableBody, TableCell, TableHead, TableRow, Tooltip, Typography } from '@mui/material'
import type { RoundSegment } from '../../services/Conversation.ts'

const fixed = (n: number, digits = 2) => (Number.isFinite(n) ? n.toFixed(digits) : '–')

function lines(s: RoundSegment): string {
  const { startLine, endLine } = s.location
  if (!startLine) return ''
  return endLine && endLine !== startLine ? `L${startLine}–L${endLine}` : `L${startLine}`
}

// A round's citations: the collapsible Sources row, listing each cited
// segment with its snippet, and the Context inspector, with the numbers
// behind every choice (score = RRF · axis × · grounding ×)
function RoundSources({ segments, onOpen }: { segments: readonly RoundSegment[]; onOpen?: (segment: RoundSegment) => void }) {
  const [open, setOpen] = useState<'sources' | 'context'>()
  const toggle = (which: 'sources' | 'context') => setOpen((o) => (o === which ? undefined : which))
  return (
    <Box>
      <Stack direction="row" spacing={0.5}>
        <Button size="small" variant={open === 'sources' ? 'contained' : 'text'} onClick={() => toggle('sources')} aria-expanded={open === 'sources'}>
          Sources ({segments.length})
        </Button>
        <Button size="small" variant={open === 'context' ? 'contained' : 'text'} onClick={() => toggle('context')} aria-expanded={open === 'context'}>
          Context
        </Button>
      </Stack>
      <Collapse in={open === 'sources'} unmountOnExit>
        <Stack spacing={1} sx={{ mt: 1 }}>
          {segments.map((s) => (
            <Box key={s.number} sx={{ pl: 1, borderLeft: 2, borderColor: 'divider' }}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline' }}>
                <Typography variant="caption" sx={{ fontWeight: 600, color: 'primary.main' }}>
                  [{s.number}]
                </Typography>
                {onOpen && (s.location.path || s.location.entryId) ? (
                  <Link component="button" variant="body2" onClick={() => onOpen(s)} sx={{ textAlign: 'left' }}>
                    {s.title}
                  </Link>
                ) : (
                  <Typography variant="body2">{s.title}</Typography>
                )}
                <Typography variant="caption" color="text.secondary">
                  {lines(s)}
                </Typography>
              </Stack>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                {s.snippet}
              </Typography>
              {s.also && s.also.length > 0 && (
                <Typography variant="caption" color="text.secondary">
                  Also in {s.also.join(', ')}
                </Typography>
              )}
            </Box>
          ))}
        </Stack>
      </Collapse>
      <Collapse in={open === 'context'} unmountOnExit>
        <Box sx={{ mt: 1, overflowX: 'auto' }}>
          <Table size="small" aria-label="Why each segment was injected" sx={{ '& th': { whiteSpace: 'nowrap' } }}>
            <TableHead>
              <TableRow>
                <TableCell>#</TableCell>
                <TableCell>Source</TableCell>
                <TableCell>Segment</TableCell>
                <TableCell>
                  <Tooltip title="Why a pinned segment is in: checked in the Chronicles view, the parent of a checked entry, or chosen by context matching (stage 1)">
                    <span>Why</span>
                  </Tooltip>
                </TableCell>
                <TableCell align="right">
                  <Tooltip title="Rank in the vector (semantic) and keyword (BM25) searches; – when that search missed it">
                    <span>Vec / Kw</span>
                  </Tooltip>
                </TableCell>
                <TableCell align="right">RRF</TableCell>
                <TableCell align="right">
                  <Tooltip title="The Chronicles ↔ Library dial's multiplier for this source">
                    <span>Axis ×</span>
                  </Tooltip>
                </TableCell>
                <TableCell align="right">
                  <Tooltip title="Earned from 👍 and 👎 on earlier replies that used it">
                    <span>Grounding ×</span>
                  </Tooltip>
                </TableCell>
                <TableCell align="right">Score</TableCell>
                <TableCell align="right">Tokens</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {segments.map((s) => (
                <TableRow key={s.number}>
                  <TableCell>{s.number}</TableCell>
                  <TableCell>{s.sourceId}</TableCell>
                  <TableCell sx={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={s.title}>
                    {s.title}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: 'nowrap' }}>{s.reason ?? (s.pinned ? 'pinned' : 'retrieval')}</TableCell>
                  <TableCell align="right">
                    {s.vectorRank ?? '–'} / {s.keywordRank ?? '–'}
                  </TableCell>
                  <TableCell align="right">{fixed(s.rrf, 4)}</TableCell>
                  <TableCell align="right">{fixed(s.axisMult)}</TableCell>
                  <TableCell align="right">{fixed(s.grounding)}</TableCell>
                  <TableCell align="right">{fixed(s.score, 4)}</TableCell>
                  <TableCell align="right">{s.tokens}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      </Collapse>
    </Box>
  )
}

export default RoundSources
