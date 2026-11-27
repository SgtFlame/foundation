// chat.composer (forge 26d-2): a Markdown message box for a channel. @
// suggests people and agents from the mentions prop (bound to a query), :
// suggests emoji, attachments are uploaded as record files of the entity
// prop, Enter sends and Shift+Enter adds a line. It emits send {text,
// mentions, files} and, while typing, typing {} at most every three
// seconds.

import { useRef, useState, type KeyboardEvent } from 'react'
import { Box, Chip, IconButton, List, ListItemButton, ListItemText, Paper, Stack, TextField, Tooltip, Typography } from '@mui/material'
import SendIcon from '@mui/icons-material/Send'
import AttachFileIcon from '@mui/icons-material/AttachFile'
import SmartToyIcon from '@mui/icons-material/SmartToy'
import type { RendererProps } from '../../points.ts'
import { Records, type FileRef } from '../../../services/Records.ts'
import { complete, mentionsIn, suggestEmoji, suggestMentions, tokenAt, typingDue, type Mentionable, type Token } from './composerText.ts'

const str = (v: unknown) => (typeof v === 'string' ? v : '')

function mentionables(v: unknown): Mentionable[] {
  if (!Array.isArray(v)) return []
  return v
    .map((x) => x as Record<string, unknown>)
    .filter((x) => x && (typeof x.id === 'string' || typeof x.id === 'number') && typeof x.name === 'string')
    .map((x) => ({ id: String(x.id), name: String(x.name).replace(/\s+/g, ''), label: str(x.label) || undefined, kind: str(x.kind) || undefined }))
}

export function ChatComposerView(p: RendererProps) {
  const people = mentionables(p.props.mentions)
  const entity = str(p.props.entity)
  const disabled = p.props.disabled === true
  const [text, setText] = useState('')
  const [token, setToken] = useState<Token | undefined>()
  const [active, setActive] = useState(0)
  const [files, setFiles] = useState<FileRef[]>([])
  const [busy, setBusy] = useState(0)
  const [failed, setFailed] = useState('')
  const input = useRef<HTMLTextAreaElement>(null)
  const picker = useRef<HTMLInputElement>(null)
  const lastTyping = useRef(0)

  const mentionList = token?.kind === 'mention' ? suggestMentions(people, token.query) : []
  const emojiList = token?.kind === 'emoji' ? suggestEmoji(token.query) : []
  const count = mentionList.length || emojiList.length

  const follow = (value: string, caret: number) => {
    setText(value)
    setToken(tokenAt(value, caret))
    setActive(0)
  }

  const choose = (i: number) => {
    if (!token) return
    const insert = token.kind === 'mention' ? `@${mentionList[i]?.name ?? ''}` : (emojiList[i]?.char ?? '')
    if (!insert || insert === '@') return
    const next = complete(text, token, insert)
    setText(next.text)
    setToken(undefined)
    requestAnimationFrame(() => {
      input.current?.focus()
      input.current?.setSelectionRange(next.caret, next.caret)
    })
  }

  const send = () => {
    const trimmed = text.trim()
    if ((!trimmed && files.length === 0) || busy > 0 || disabled) return
    p.emit('send', { text: trimmed, mentions: mentionsIn(trimmed, people), files })
    setText('')
    setFiles([])
    setToken(undefined)
    lastTyping.current = 0
  }

  const onKeyDown = (e: KeyboardEvent) => {
    if (count > 0) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => (a + 1) % count); return }
      if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => (a + count - 1) % count); return }
      if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); choose(active); return }
      if (e.key === 'Escape') { e.preventDefault(); setToken(undefined); return }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      send()
    }
  }

  const attach = (picked: File[]) => {
    if (!entity || picked.length === 0) return
    setBusy((b) => b + picked.length)
    setFailed('')
    for (const f of picked) {
      Records.upload(entity, f)
        .then((r) => r.file && setFiles((fs) => [...fs, r.file!]), (err) => setFailed((err as Error).message))
        .finally(() => setBusy((b) => b - 1))
    }
  }

  return (
    <Stack sx={{ position: 'relative', width: '100%' }} spacing={0.5}>
      {count > 0 && (
        <Paper elevation={4} sx={{ position: 'absolute', bottom: '100%', left: 0, mb: 0.5, minWidth: 260, maxHeight: 280, overflow: 'auto', zIndex: 2 }}>
          <List dense disablePadding aria-label={token?.kind === 'mention' ? 'People' : 'Emoji'}>
            {mentionList.map((m, i) => (
              <ListItemButton key={m.id} selected={i === active} onMouseDown={(e) => { e.preventDefault(); choose(i) }}>
                {m.kind === 'agent' && <SmartToyIcon fontSize="small" sx={{ mr: 1, color: 'text.secondary' }} />}
                <ListItemText primary={m.label ?? m.name} secondary={`@${m.name}`} />
              </ListItemButton>
            ))}
            {emojiList.map((e, i) => (
              <ListItemButton key={e.name} selected={i === active} onMouseDown={(ev) => { ev.preventDefault(); choose(i) }}>
                <Box component="span" sx={{ mr: 1, fontSize: 18 }}>{e.char}</Box>
                <ListItemText primary={`:${e.name}:`} />
              </ListItemButton>
            ))}
          </List>
        </Paper>
      )}
      {(files.length > 0 || busy > 0 || failed) && (
        <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
          {files.map((f) => (
            <Chip key={f.id} size="small" label={f.name} onDelete={() => setFiles((fs) => fs.filter((x) => x.id !== f.id))} />
          ))}
          {busy > 0 && <Typography variant="caption" color="text.secondary">Uploading…</Typography>}
          {failed && <Typography variant="caption" color="error">{failed}</Typography>}
        </Stack>
      )}
      <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-end' }}>
        {entity && (
          <>
            <input ref={picker} type="file" multiple hidden onChange={(e) => { attach(Array.from(e.target.files ?? [])); e.target.value = '' }} />
            <Tooltip title="Attach files">
              <span>
                <IconButton aria-label="Attach files" disabled={disabled} onClick={() => picker.current?.click()}>
                  <AttachFileIcon />
                </IconButton>
              </span>
            </Tooltip>
          </>
        )}
        <TextField
          fullWidth
          multiline
          maxRows={10}
          size="small"
          disabled={disabled}
          placeholder={str(p.props.placeholder) || 'Write a message. @ to mention, : for emoji'}
          value={text}
          inputRef={input}
          onChange={(e) => {
            const el = e.target as HTMLTextAreaElement
            follow(el.value, el.selectionStart ?? el.value.length)
            const now = Date.now()
            if (el.value.trim() && typingDue(lastTyping.current, now) && p.handles('typing')) {
              lastTyping.current = now
              p.emit('typing', {})
            }
          }}
          onSelect={(e) => {
            const el = e.target as HTMLTextAreaElement
            setToken(tokenAt(el.value, el.selectionStart ?? el.value.length))
          }}
          onBlur={() => setToken(undefined)}
          onKeyDown={onKeyDown}
          onPaste={(e) => {
            const pasted = Array.from(e.clipboardData.files)
            if (pasted.length > 0 && entity) {
              e.preventDefault()
              attach(pasted)
            }
          }}
          slotProps={{ htmlInput: { 'aria-label': str(p.props.label) || 'Message' } }}
        />
        <Tooltip title="Send (Enter)">
          <span>
            <IconButton color="primary" aria-label="Send" onClick={send} disabled={disabled || busy > 0 || (!text.trim() && files.length === 0)}>
              <SendIcon />
            </IconButton>
          </span>
        </Tooltip>
      </Stack>
    </Stack>
  )
}

export default ChatComposerView
