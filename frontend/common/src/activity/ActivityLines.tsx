// The person's gestures in the chat's transcript (forge 15d), as quiet,
// timestamped comments between messages: those since the last round,
// read live and dismissable, and those a round was sent with. Since 15g
// a pencil marks one of a gesture's values to ask for each run when what
// they did is automated.

import { useState } from 'react'
import { Box, Button, Chip, IconButton, Link, Popover, Radio, Stack, TextField, Tooltip, Typography } from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import EditNoteIcon from '@mui/icons-material/EditNote'
import { useQuery } from '../live/index.ts'
import { ActivityCalls, type Activity, type ActivityEntry, type ActivityMark } from '../services/Activity.ts'
import { log } from '../logging/index.ts'

const activityLog = log.In('activity')

// A value of a gesture marked to ask for each run, as a line shows it
export interface LineAsk {
  entry?: string
  field?: string
  name: string
  question: string
  default?: unknown
}

// What a line shows of a gesture
export interface GestureLine {
  id: string
  start: string
  summary: string
  repeats?: number
  dismissed?: boolean
  asks?: LineAsk[]
  entries?: ActivityEntry[] // the pending gestures', whose values the pencil lists
}

// How many lines show before the rest collapse into "and n more"
const shown = 5

// A comment's text: the summary with its long parentheses (a record's
// every field) folded to (…), the whole summary in its tooltip. The model
// is sent the whole summary.
export function shortSummary(summary: string): string {
  return summary.replace(/ \(([^()]*)\)/g, (all, inner: string) => (inner.length > 40 ? ' (…)' : all))
}

function clock(time: string): string {
  return new Date(time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// A value as text: a list's items joined
export function valueText(v: unknown): string {
  if (v === undefined || v === null) return ''
  if (Array.isArray(v)) return v.map((x) => String(x)).join(', ')
  return typeof v === 'object' ? JSON.stringify(v) : String(v)
}

// The values a gesture's commands changed, which the pencil can mark
interface Candidate {
  entry: string
  field: string
  name: string
  value?: unknown
  omitted?: boolean
}

function candidates(entries: readonly ActivityEntry[] | undefined): Candidate[] {
  const out: Candidate[] = []
  for (const e of entries ?? []) {
    if (e.kind !== 'command') continue
    for (const f of e.fields ?? []) {
      if (!out.some((c) => c.name === f.name && valueText(c.value) === valueText(f.value))) {
        out.push({ entry: e.id, field: f.id, name: f.name, value: f.value, omitted: f.omitted })
      }
    }
  }
  return out
}

// The pencil's popover: pick one of the gesture's values, word the
// question and keep this time's value as the default answer
function AskEditor({ gesture, anchor, onClose, onMark }: { gesture: GestureLine; anchor: HTMLElement; onClose: () => void; onMark: (mark: ActivityMark) => void }) {
  const values = candidates(gesture.entries)
  const [picked, setPicked] = useState(0)
  const chosen = values[picked]
  const marked = gesture.asks?.find((a) => a.entry === chosen?.entry && a.field === chosen?.field)
  const [question, setQuestion] = useState(marked?.question ?? (chosen ? `Which ${chosen.name.toLowerCase()}?` : ''))
  const [answer, setAnswer] = useState(valueText(marked?.default ?? chosen?.value))
  const pick = (i: number) => {
    const c = values[i]
    const m = gesture.asks?.find((a) => a.entry === c.entry && a.field === c.field)
    setPicked(i)
    setQuestion(m?.question ?? `Which ${c.name.toLowerCase()}?`)
    setAnswer(valueText(m?.default ?? c.value))
  }
  return (
    <Popover open anchorEl={anchor} onClose={onClose} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}>
      <Stack spacing={1.5} sx={{ p: 2, width: 380 }} role="dialog" aria-label="Ask for this each run">
        <Typography variant="subtitle2">Ask for this each run</Typography>
        <Typography variant="caption" color="text.secondary">
          This time's value was yours. When this is automated, the automation asks for it each time it runs, with this time's value as the default.
        </Typography>
        <Stack role="radiogroup" aria-label="Value">
          {values.map((c, i) => (
            <Stack key={`${c.entry}/${c.field}`} direction="row" sx={{ alignItems: 'center', cursor: 'pointer' }} onClick={() => pick(i)}>
              <Radio size="small" checked={i === picked} slotProps={{ input: { 'aria-label': c.name } }} />
              <Typography variant="body2">
                <b>{c.name}</b>: {c.omitted ? <i>not kept</i> : valueText(c.value) || <i>empty</i>}
              </Typography>
            </Stack>
          ))}
        </Stack>
        <TextField size="small" label="The question" value={question} onChange={(e) => setQuestion(e.target.value)} slotProps={{ htmlInput: { maxLength: 300 } }} />
        <TextField size="small" label="Default answer" value={answer} onChange={(e) => setAnswer(e.target.value)} helperText="Leave it as it is to keep this time's value" />
        <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end' }}>
          {marked && (
            <Button size="small" color="inherit" onClick={() => { onMark({ gesture: gesture.id, entry: chosen.entry, field: chosen.field, remove: true }); onClose() }}>
              Don't ask
            </Button>
          )}
          <Button size="small" onClick={onClose} color="inherit">Cancel</Button>
          <Button
            size="small"
            variant="contained"
            disabled={!chosen || !question.trim()}
            onClick={() => {
              const same = answer === valueText(chosen.value)
              onMark({ gesture: gesture.id, entry: chosen.entry, field: chosen.field, question: question.trim(), ...(same ? {} : { default: answer }) })
              onClose()
            }}
          >
            Ask each run
          </Button>
        </Stack>
      </Stack>
    </Popover>
  )
}

// The lines, newest last; past five, the newest five show under "and n
// more". onDismiss puts a ✕ on each line not yet dismissed, and onMark a
// pencil on each with values to mark. A line's marks show as chips.
export function ActivityLines({ gestures, label, onDismiss, onMark }: { gestures: readonly GestureLine[]; label: string; onDismiss?: (id: string) => void; onMark?: (mark: ActivityMark) => void }) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<{ gesture: GestureLine; anchor: HTMLElement }>()
  if (gestures.length === 0) return null
  const hidden = open ? 0 : Math.max(0, gestures.length - shown)
  return (
    <Stack component="ul" aria-label={label} sx={{ listStyle: 'none', m: 0, px: 2, py: 0.5, gap: 0.25 }}>
      {hidden > 0 && (
        <Box component="li">
          <Link component="button" variant="caption" color="text.secondary" onClick={() => setOpen(true)}>
            and {hidden} more
          </Link>
        </Box>
      )}
      {gestures.slice(hidden).map((g) => (
        <Box component="li" key={g.id} aria-label={g.summary} sx={{ color: 'text.secondary', '&:hover .dismiss': { opacity: 1 } }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
            <Typography variant="caption" sx={{ fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>
              {clock(g.start)}
            </Typography>
            <Typography
              variant="caption"
              sx={{ fontStyle: 'italic', textDecoration: g.dismissed ? 'line-through' : undefined, opacity: g.dismissed ? 0.6 : 1, wordBreak: 'break-word' }}
              title={g.dismissed ? `Dismissed: never sent to the assistant. ${g.summary}` : g.summary}
            >
              {shortSummary(g.summary)}
              {g.repeats ? ` (×${g.repeats + 1})` : ''}
            </Typography>
            {onMark && !g.dismissed && candidates(g.entries).length > 0 && (
              <Tooltip title="Ask for one of these values each run when this is automated">
                <IconButton
                  className="dismiss"
                  size="small"
                  sx={{ opacity: 0.4, p: 0.25 }}
                  aria-label={`Ask each run: ${g.summary}`}
                  onClick={(e) => setEditing({ gesture: g, anchor: e.currentTarget })}
                >
                  <EditNoteIcon sx={{ fontSize: 16 }} />
                </IconButton>
              </Tooltip>
            )}
            {onDismiss && !g.dismissed && (
              <Tooltip title="Dismiss: never send this to the assistant">
                <IconButton className="dismiss" size="small" sx={{ opacity: 0.4, p: 0.25 }} aria-label={`Dismiss ${g.summary}`} onClick={() => onDismiss(g.id)}>
                  <CloseIcon sx={{ fontSize: 14 }} />
                </IconButton>
              </Tooltip>
            )}
          </Stack>
          {!g.dismissed && g.asks && g.asks.length > 0 && (
            <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', pl: 7, pt: 0.25 }} aria-label="Asked each run">
              {g.asks.map((a) => (
                <Chip
                  key={`${a.entry ?? ''}/${a.field ?? a.name}`}
                  size="small"
                  variant="outlined"
                  color="primary"
                  icon={<EditNoteIcon />}
                  label={`asks: ${a.question}${a.default !== undefined && a.default !== null ? ` (${valueText(a.default)})` : ''}`}
                  title={`Asked each run for ${a.name}`}
                  onDelete={onMark && a.entry && a.field ? () => onMark({ gesture: g.id, entry: a.entry!, field: a.field!, remove: true }) : undefined}
                />
              ))}
            </Stack>
          )}
        </Box>
      ))}
      {editing && onMark && <AskEditor gesture={editing.gesture} anchor={editing.anchor} onClose={() => setEditing(undefined)} onMark={onMark} />}
    </Stack>
  )
}

// What the person did since the last round, followed live: it goes with
// the next message unless dismissed. Nothing shows while recording is off.
export function PendingActivity({ after }: { after: string }) {
  const { data } = useQuery<Activity>('activity.mine', { after })
  if (!data?.recording || data.gestures.length === 0) return null
  const dismiss = (id: string) => {
    ActivityCalls.dismiss([id]).catch((err: unknown) => activityLog.fromError(err).warn('cannot dismiss gesture %s', id))
  }
  const mark = (m: ActivityMark) => {
    ActivityCalls.ask(m).catch((err: unknown) => activityLog.fromError(err).warn('cannot mark gesture %s', m.gesture))
  }
  return <ActivityLines gestures={data.gestures} label="What you did since your last message" onDismiss={dismiss} onMark={mark} />
}
