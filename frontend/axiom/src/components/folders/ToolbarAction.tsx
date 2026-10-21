import type { ReactNode } from 'react'
import { IconButton, Tooltip } from '@mui/material'

// A small icon button with a tooltip that still shows while disabled; the
// button carries the title as its accessible name
function ToolbarAction({
  title,
  disabled,
  onClick,
  children,
}: {
  title: string
  disabled?: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Tooltip title={title}>
      <span>
        <IconButton size="small" aria-label={title} disabled={disabled} onClick={onClick}>
          {children}
        </IconButton>
      </span>
    </Tooltip>
  )
}

export default ToolbarAction
