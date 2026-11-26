import { Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from '@mui/material'
import { useWorkspace } from './workspaceStore.ts'
import { useWorkspaceWindows, workspaceWindows } from './windows.ts'

// Asked when this window opens a workspace another window already shows
// (forge 16b question 9): follow it live, or leave this window unsaved
function WorkspaceWindowsDialog() {
  const { current } = useWorkspace()
  const windows = useWorkspaceWindows()
  if (windows.mode !== 'asking' || !current || windows.workspace !== current.id) return null
  const other = windows.others === 1 ? 'another window' : `${windows.others} other windows`
  return (
    <Dialog open maxWidth="sm" aria-labelledby="ws-windows-title">
      <DialogTitle id="ws-windows-title">{current.name} is open in {other}</DialogTitle>
      <DialogContent>
        <DialogContentText sx={{ mb: 1.5 }}>
          <b>Live follow</b>: this window shows the same editors and layout, and a change in either window shows in both.
        </DialogContentText>
        <DialogContentText>
          <b>Don't save this window</b>: open and arrange editors here freely; nothing this window does changes the workspace.
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={() => workspaceWindows.choose('unsaved')}>Don't save this window</Button>
        <Button variant="contained" onClick={() => workspaceWindows.choose('follow')} autoFocus>Live follow</Button>
      </DialogActions>
    </Dialog>
  )
}

export default WorkspaceWindowsDialog
