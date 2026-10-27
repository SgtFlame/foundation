import { useState } from 'react'
import { Button, Stack, TextField, Typography } from '@mui/material'
import type { ClientToolCardProps } from '../editors/chat/points.ts'

interface Choice {
  label: string
  value?: string
  subject?: { type: string; id: string; label?: string }
}

// chat.askChoice: the model's question with its candidates as buttons, and
// a typed answer for anything else
function AskChoiceCard({ call, answer }: ClientToolCardProps) {
  const [other, setOther] = useState('')
  const question = typeof call.args.question === 'string' ? call.args.question : 'Which one?'
  const choices = (Array.isArray(call.args.choices) ? call.args.choices : []) as Choice[]
  return (
    <Stack spacing={1}>
      <Typography variant="body2">{question}</Typography>
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}>
        {choices.map((c, i) => (
          <Button key={i} size="small" variant="outlined" onClick={() => answer({ choice: c.label, value: c.value ?? c.label, subject: c.subject })}>
            {c.label}
          </Button>
        ))}
      </Stack>
      <Stack
        direction="row"
        spacing={1}
        component="form"
        onSubmit={(e) => {
          e.preventDefault()
          if (other.trim()) answer({ text: other.trim() })
        }}
      >
        <TextField size="small" placeholder="Something else…" value={other} onChange={(e) => setOther(e.target.value)} sx={{ flex: 1 }} slotProps={{ htmlInput: { 'aria-label': 'Another answer' } }} />
        <Button size="small" type="submit" disabled={!other.trim()}>
          Answer
        </Button>
      </Stack>
    </Stack>
  )
}

export default AskChoiceCard
