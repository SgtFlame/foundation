import { useEffect, useRef } from 'react'
import { Box, Stack, alpha } from '@mui/material'
import { defaultValueCtx, Editor, editorViewCtx, rootCtx } from '@milkdown/kit/core'
import { history } from '@milkdown/kit/plugin/history'
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener'
import { commonmark } from '@milkdown/kit/preset/commonmark'
import { gfm } from '@milkdown/kit/preset/gfm'
import { Milkdown, MilkdownProvider, useEditor, useInstance } from '@milkdown/react'
import type { CollabSession } from '../collab/CollabSession.ts'
import { useCollabState } from '../collab/useCollabSession.ts'
import WysiwygToolbar from './WysiwygToolbar.tsx'
import { YTextBridge } from './yTextBridge.ts'

function WysiwygEditor({ session, readOnly }: { session: CollabSession; readOnly: boolean }) {
  const bridge = useRef<YTextBridge>(undefined)

  useEditor(
    (root) =>
      Editor.make()
        .config((ctx) => {
          ctx.set(rootCtx, root)
          ctx.set(defaultValueCtx, session.ytext.toString())
          ctx.get(listenerCtx).markdownUpdated((_ctx, markdown) => bridge.current?.onLocal(markdown))
        })
        .use(commonmark)
        .use(gfm)
        .use(history)
        .use(listener),
    [session],
  )

  const [loading, getEditor] = useInstance()
  useEffect(() => {
    const editor = getEditor()
    if (loading || !editor) return
    const created = new YTextBridge(editor, session.ytext)
    bridge.current = created
    return () => {
      created.destroy()
      bridge.current = undefined
    }
  }, [loading, getEditor, session])

  useEffect(() => {
    if (loading) return
    getEditor()?.action((ctx) => ctx.get(editorViewCtx).setProps({ editable: () => !readOnly }))
  }, [loading, getEditor, readOnly])

  return <Milkdown />
}

// A WYSIWYG view in Milkdown, synced with the session's shared text through markdown
function WysiwygView({ session }: { session: CollabSession }) {
  const state = useCollabState(session)
  const readOnly = state.status === 'closed' || state.readOnly

  return (
    <MilkdownProvider>
      <Stack sx={{ height: '100%', minHeight: 0 }}>
        <WysiwygToolbar disabled={readOnly} />
        <Box
          sx={(theme) => ({
            flexGrow: 1,
            overflow: 'auto',
            '& .milkdown': { maxWidth: 750, mx: 'auto', px: 2, py: 3 },
            '& .ProseMirror': {
              outline: 'none',
              ...theme.typography.body1,
              lineHeight: 1.6,
              '& h1': { ...theme.typography.h4, fontWeight: 700, mt: 2, mb: 1 },
              '& h2': { ...theme.typography.h5, fontWeight: 700, mt: 2, mb: 1 },
              '& h3': { ...theme.typography.h6, mt: 1.5, mb: 1 },
              '& h4, & h5, & h6': { ...theme.typography.subtitle1, fontWeight: 600, mt: 1.5 },
              '& p': { my: 1 },
              '& a': { color: theme.palette.primary.main },
              '& code': {
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
                fontSize: '0.9em',
                backgroundColor: alpha(theme.palette.text.primary, 0.06),
                borderRadius: '3px',
                px: 0.5,
              },
              '& pre': {
                backgroundColor: alpha(theme.palette.text.primary, 0.06),
                p: 1.5,
                borderRadius: 1,
                overflowX: 'auto',
                '& code': { backgroundColor: 'transparent', p: 0 },
              },
              '& blockquote': {
                borderLeft: `3px solid ${theme.palette.divider}`,
                color: theme.palette.text.secondary,
                m: 0,
                pl: 2,
              },
              '& li[data-item-type="task"]': { listStyle: 'none', position: 'relative' },
              '& li[data-item-type="task"]::before': {
                content: '"☐"',
                position: 'absolute',
                left: '-1.4em',
              },
              '& li[data-item-type="task"][data-checked="true"]::before': { content: '"☑"' },
              '& li[data-item-type="task"][data-checked="true"]': {
                color: theme.palette.text.secondary,
                textDecoration: 'line-through',
              },
              '& hr': { border: 'none', borderTop: `1px solid ${theme.palette.divider}`, my: 2 },
              '& table': { borderCollapse: 'collapse', my: 1 },
              '& th, & td': { border: `1px solid ${theme.palette.divider}`, px: 1, py: 0.5, minWidth: 60 },
              '& th': { backgroundColor: alpha(theme.palette.text.primary, 0.04) },
              '& img': { maxWidth: '100%' },
            },
          })}
        >
          <WysiwygEditor session={session} readOnly={readOnly} />
        </Box>
      </Stack>
    </MilkdownProvider>
  )
}

export default WysiwygView
