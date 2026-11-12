import { type ReactNode, useState } from 'react'
import { Divider, Stack } from '@mui/material'
import CodeIcon from '@mui/icons-material/Code'
import FormatBoldIcon from '@mui/icons-material/FormatBold'
import FormatItalicIcon from '@mui/icons-material/FormatItalic'
import FormatListBulletedIcon from '@mui/icons-material/FormatListBulleted'
import FormatListNumberedIcon from '@mui/icons-material/FormatListNumbered'
import FormatQuoteIcon from '@mui/icons-material/FormatQuote'
import HorizontalRuleIcon from '@mui/icons-material/HorizontalRule'
import LinkIcon from '@mui/icons-material/Link'
import StrikethroughSIcon from '@mui/icons-material/StrikethroughS'
import TableChartIcon from '@mui/icons-material/TableChart'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/kit/ctx'
import {
  insertHrCommand,
  toggleEmphasisCommand,
  toggleInlineCodeCommand,
  toggleLinkCommand,
  toggleStrongCommand,
  wrapInBlockquoteCommand,
  wrapInBulletListCommand,
  wrapInHeadingCommand,
  wrapInOrderedListCommand,
} from '@milkdown/kit/preset/commonmark'
import { insertTableCommand, toggleStrikethroughCommand } from '@milkdown/kit/preset/gfm'
import { callCommand } from '@milkdown/kit/utils'
import { useInstance } from '@milkdown/react'
import ToolbarAction from '../../../components/folders/ToolbarAction.tsx'
import LinkDialog from './LinkDialog.tsx'

// Formatting actions for the WYSIWYG view
function WysiwygToolbar({ disabled }: { disabled: boolean }) {
  const [loading, getEditor] = useInstance()
  const [linking, setLinking] = useState(false)

  const run = (command: (ctx: Ctx) => boolean) => {
    getEditor()?.action((ctx) => {
      command(ctx)
      ctx.get(editorViewCtx).focus()
    })
  }

  const off = disabled || loading
  const action = (title: string, icon: ReactNode, onClick: () => void) => (
    <ToolbarAction key={title} title={title} disabled={off} onClick={onClick}>
      {icon}
    </ToolbarAction>
  )
  const heading = (level: number) => (
    <ToolbarAction
      key={level}
      title={`Heading ${level}`}
      disabled={off}
      onClick={() => run(callCommand(wrapInHeadingCommand.key, level))}
    >
      <span style={{ fontSize: 13, fontWeight: 700, width: 20 }}>H{level}</span>
    </ToolbarAction>
  )

  return (
    <Stack
      direction="row"
      sx={{ alignItems: 'center', gap: 0.25, px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider', flexWrap: 'wrap' }}
    >
      {action('Bold', <FormatBoldIcon fontSize="small" />, () => run(callCommand(toggleStrongCommand.key)))}
      {action('Italic', <FormatItalicIcon fontSize="small" />, () => run(callCommand(toggleEmphasisCommand.key)))}
      {action('Strikethrough', <StrikethroughSIcon fontSize="small" />, () => run(callCommand(toggleStrikethroughCommand.key)))}
      {action('Inline code', <CodeIcon fontSize="small" />, () => run(callCommand(toggleInlineCodeCommand.key)))}
      {action('Link', <LinkIcon fontSize="small" />, () => setLinking(true))}
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      {[1, 2, 3].map(heading)}
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      {action('Bulleted list', <FormatListBulletedIcon fontSize="small" />, () => run(callCommand(wrapInBulletListCommand.key)))}
      {action('Numbered list', <FormatListNumberedIcon fontSize="small" />, () => run(callCommand(wrapInOrderedListCommand.key)))}
      {action('Quote', <FormatQuoteIcon fontSize="small" />, () => run(callCommand(wrapInBlockquoteCommand.key)))}
      {action('Table', <TableChartIcon fontSize="small" />, () => run(callCommand(insertTableCommand.key)))}
      {action('Divider', <HorizontalRuleIcon fontSize="small" />, () => run(callCommand(insertHrCommand.key)))}
      <LinkDialog
        open={linking}
        onClose={() => setLinking(false)}
        onLink={(href) => {
          setLinking(false)
          run(callCommand(toggleLinkCommand.key, { href }))
        }}
      />
    </Stack>
  )
}

export default WysiwygToolbar
