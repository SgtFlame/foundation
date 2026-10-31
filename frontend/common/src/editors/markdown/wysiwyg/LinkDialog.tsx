import { useState } from 'react'
import { Button, Dialog, DialogActions, DialogContent, DialogTitle, TextField } from '@mui/material'

// Asks for a link's URL
function LinkDialog({ open, onClose, onLink }: { open: boolean; onClose: () => void; onLink: (href: string) => void }) {
  const [href, setHref] = useState('https://')
  const submit = () => {
    if (href.trim()) onLink(href.trim())
  }

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>Link</DialogTitle>
      <DialogContent>
        <TextField
          autoFocus
          fullWidth
          margin="dense"
          label="URL"
          value={href}
          onChange={(event) => setHref(event.target.value)}
          onKeyDown={(event) => event.key === 'Enter' && submit()}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button onClick={submit}>Link</Button>
      </DialogActions>
    </Dialog>
  )
}

export default LinkDialog
