import { useState } from 'react'
import {
  Button, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Radio, RadioGroup, Stack, TextField, Typography,
} from '@mui/material'
import type { ClientToolCardProps } from '../editors/chat/points.ts'

// One question of chat.askForm
export interface FormQuestion {
  id: string
  question: string
  kind?: 'text' | 'choice'
  choices?: { label: string; value?: string }[]
  default?: string
  help?: string
}

export function questionsOf(args: Record<string, unknown>): FormQuestion[] {
  const raw = Array.isArray(args.questions) ? (args.questions as unknown[]) : []
  return raw
    .filter((q): q is Record<string, unknown> => typeof q === 'object' && q !== null)
    .map((q, i) => ({
      id: typeof q.id === 'string' && q.id ? q.id : `q${i + 1}`,
      question: typeof q.question === 'string' ? q.question : `Question ${i + 1}`,
      kind: q.kind === 'choice' || (Array.isArray(q.choices) && q.choices.length > 0) ? 'choice' : 'text',
      choices: Array.isArray(q.choices) ? (q.choices as FormQuestion['choices']) : undefined,
      default: q.default === undefined || q.default === null ? undefined : String(q.default),
      help: typeof q.help === 'string' ? q.help : undefined,
    }))
}

// The answers a form starts with: each question's default
function defaults(qs: readonly FormQuestion[]): Record<string, string> {
  return Object.fromEntries(qs.map((q) => [q.id, q.default ?? '']))
}

function Question({ q, value, onChange }: { q: FormQuestion; value: string; onChange: (v: string) => void }) {
  if (q.kind === 'choice') {
    const values = (q.choices ?? []).map((c) => c.value ?? c.label)
    const other = value !== '' && !values.includes(value)
    return (
      <Stack spacing={0.5}>
        <Typography variant="subtitle2">{q.question}</Typography>
        {q.help && <Typography variant="caption" color="text.secondary">{q.help}</Typography>}
        <RadioGroup value={other ? '' : value} onChange={(_, v) => onChange(v)} aria-label={q.question}>
          {(q.choices ?? []).map((c) => (
            <FormControlLabel key={c.value ?? c.label} value={c.value ?? c.label} control={<Radio size="small" />} label={c.label} />
          ))}
        </RadioGroup>
        <TextField size="small" placeholder="Something else…" value={other ? value : ''} onChange={(e) => onChange(e.target.value)} slotProps={{ htmlInput: { 'aria-label': `${q.question}: another answer` } }} />
      </Stack>
    )
  }
  return (
    <TextField
      size="small"
      label={q.question}
      helperText={q.help}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      multiline
      maxRows={6}
      fullWidth
    />
  )
}

// chat.askForm: several questions asked together, once (forge 15g). The
// card opens a dialog with every question, choices as options and text as
// fields filled with their defaults, and answers {answers: {id: value}}.
function AskFormCard({ call, answer }: ClientToolCardProps) {
  const title = typeof call.args.title === 'string' && call.args.title ? call.args.title : 'A few questions'
  const qs = questionsOf(call.args)
  const [open, setOpen] = useState(true)
  const [values, setValues] = useState<Record<string, string>>(() => defaults(qs))
  const unanswered = qs.filter((q) => !values[q.id]?.trim()).length
  return (
    <Stack spacing={1}>
      <Typography variant="body2">
        {title}: {qs.length} {qs.length === 1 ? 'question' : 'questions'}
      </Typography>
      <Stack direction="row" spacing={1}>
        <Button size="small" variant="outlined" onClick={() => setOpen(true)}>
          Answer
        </Button>
      </Stack>
      <Dialog open={open} onClose={() => setOpen(false)} maxWidth="sm" fullWidth aria-label={title}>
        <DialogTitle>{title}</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5} sx={{ pt: 1 }}>
            {qs.map((q) => (
              <Question key={q.id} q={q} value={values[q.id] ?? ''} onChange={(v) => setValues((all) => ({ ...all, [q.id]: v }))} />
            ))}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => answer({ cancelled: true })}>
            Skip
          </Button>
          <Button
            variant="contained"
            onClick={() => answer({ answers: Object.fromEntries(qs.map((q) => [q.id, (values[q.id] ?? '').trim()])) })}
            title={unanswered ? `${unanswered} left empty` : undefined}
          >
            Send answers
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  )
}

export default AskFormCard
