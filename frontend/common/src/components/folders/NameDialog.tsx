import { useState, type ReactNode } from 'react'
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
} from '@mui/material'

// Asks for a name; children add further fields
function NameDialog({
  open,
  title,
  onClose,
  onCreate,
  children,
}: {
  open: boolean
  title: string
  onClose: () => void
  onCreate: (name: string) => void
  children?: ReactNode
}) {
  const [name, setName] = useState('')
  const valid = name.trim() !== ''

  const submit = () => {
    if (!valid) return
    onCreate(name.trim())
    setName('')
  }

  return (
    <Dialog open={open} onClose={onClose}>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        <TextField
          autoFocus
          fullWidth
          margin="dense"
          label="Name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => event.key === 'Enter' && submit()}
        />
        {children}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button onClick={submit} disabled={!valid}>
          Create
        </Button>
      </DialogActions>
    </Dialog>
  )
}

export default NameDialog
