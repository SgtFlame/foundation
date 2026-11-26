import { useEffect, useState } from 'react'
import { Alert, Box, CircularProgress, Dialog, DialogContent, DialogTitle, Stack, Typography } from '@mui/material'
import { log } from '../../../logging/index.ts'
import { Usage, type CaptureResponse } from '../../../services/Usage.ts'

function Block({ title, text }: { title: string; text: string }) {
  return (
    <Box>
      <Typography variant="overline">{title}</Typography>
      <Box component="pre" sx={{ m: 0, p: 1, bgcolor: 'action.hover', fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
        {text}
      </Box>
    </Box>
  )
}

// One record's capture, loaded when shown
function CaptureContent({ recordId }: { recordId: string }) {
  const [capture, setCapture] = useState<CaptureResponse>()
  const [error, setError] = useState<string>()

  useEffect(() => {
    Usage.capture({ recordId })
      .then(setCapture)
      .catch((err: unknown) => {
        log.In('usage').fromError(err).With('recordId', recordId).warn('cannot read the capture of %s', recordId)
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [recordId])

  if (error) return <Alert severity="warning">{error}</Alert>
  if (!capture) return <CircularProgress size={24} />
  return (
    <Stack spacing={1}>
      {capture.system && <Block title="System" text={capture.system} />}
      {capture.messages?.map((m, i) => <Block key={i} title={m.role} text={m.content} />)}
      <Block title="Reply" text={capture.reply ?? ''} />
    </Stack>
  )
}

// What one captured chat call was sent and what it replied
function CaptureDialog({ recordId, onClose }: { recordId?: string; onClose: () => void }) {
  return (
    <Dialog open={!!recordId} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>Captured call</DialogTitle>
      <DialogContent>{recordId && <CaptureContent key={recordId} recordId={recordId} />}</DialogContent>
    </Dialog>
  )
}

export default CaptureDialog
