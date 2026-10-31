import { useState, type ReactNode } from 'react'
import { IconButton, List, ListItemButton, ListItemText, Paper, Stack, TextField, Tooltip } from '@mui/material'
import SendIcon from '@mui/icons-material/Send'
import StopIcon from '@mui/icons-material/Stop'
import type { ChatCommandContribution } from '../../editors/chat/points.ts'

// The message box: Enter sends and Shift+Enter adds a line; "/" lists the
// chat.command contributions; Stop aborts the reply. tools (the level
// toggle, ⚙) sit above it.
function ChatComposer({
  onSend,
  onCommand,
  onStop,
  pending,
  commands,
  tools,
}: {
  onSend: (text: string) => void
  onCommand: (command: ChatCommandContribution, arg: string) => void
  onStop: () => void
  pending: boolean
  commands: readonly ChatCommandContribution[]
  tools?: ReactNode
}) {
  const [text, setText] = useState('')
  const command = /^\/(\S*)(?:\s+([\s\S]*))?$/.exec(text)
  const matching = command && !text.includes('\n') ? commands.filter((c) => c.id.startsWith(command[1])) : []
  const exact = command ? commands.find((c) => c.id === command[1]) : undefined

  const submit = () => {
    const trimmed = text.trim()
    if (!trimmed) return
    if (command) {
      const run = exact ?? (matching.length === 1 ? matching[0] : undefined)
      if (!run) return
      onCommand(run, command[2] ?? '')
      setText('')
      return
    }
    if (pending) return
    onSend(trimmed)
    setText('')
  }

  return (
    <Stack sx={{ position: 'relative', p: 1, borderTop: 1, borderColor: 'divider' }} spacing={1}>
      {matching.length > 0 && !exact?.usage && (
        <Paper elevation={4} sx={{ position: 'absolute', bottom: '100%', left: 8, mb: 0.5, minWidth: 280, zIndex: 1 }}>
          <List dense disablePadding>
            {matching.map((c) => (
              <ListItemButton key={c.id} onClick={() => setText(`/${c.id}${c.usage ? ' ' : ''}`)}>
                <ListItemText primary={`/${c.id}${c.usage ? ' ' + c.usage : ''}`} secondary={c.description} />
              </ListItemButton>
            ))}
          </List>
        </Paper>
      )}
      {tools && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          {tools}
        </Stack>
      )}
      <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-end' }}>
        <TextField
          fullWidth
          multiline
          maxRows={8}
          size="small"
          placeholder="Message the assistant. / for commands"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              submit()
            }
          }}
          slotProps={{ htmlInput: { 'aria-label': 'Message' } }}
        />
        {pending ? (
          <Tooltip title="Stop the reply">
            <IconButton onClick={onStop} color="error" aria-label="Stop the reply">
              <StopIcon />
            </IconButton>
          </Tooltip>
        ) : (
          <Tooltip title="Send (Enter)">
            <span>
              <IconButton onClick={submit} color="primary" disabled={!text.trim()} aria-label="Send">
                <SendIcon />
              </IconButton>
            </span>
          </Tooltip>
        )}
      </Stack>
    </Stack>
  )
}

export default ChatComposer
