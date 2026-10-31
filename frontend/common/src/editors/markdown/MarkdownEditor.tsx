import { lazy, Suspense, useCallback, useEffect, useMemo } from 'react'
import { Box, CircularProgress, Stack } from '@mui/material'
import { Layout, Model, type IJsonModel, type TabNode } from 'flexlayout-react'
import type * as Y from 'yjs'
import { useWorkbenchContext, useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import type { CollabSession } from './collab/CollabSession.ts'
import { useCollabSession, useCollabState } from './collab/useCollabSession.ts'
import MarkdownStatusBar from './MarkdownStatusBar.tsx'
import type { MarkdownObject } from './MarkdownObject.ts'

// Each view is its own chunk, so its editor library loads only when shown
const LivePreviewView = lazy(() => import('./live/LivePreviewView.tsx'))
const SourceView = lazy(() => import('./source/SourceView.tsx'))
const WysiwygView = lazy(() => import('./wysiwyg/WysiwygView.tsx'))

const json: IJsonModel = {
  global: { tabEnableClose: false, tabSetEnableMaximize: false },
  borders: [],
  layout: {
    type: 'row',
    weight: 100,
    children: [
      {
        type: 'tabset',
        weight: 100,
        children: [
          { type: 'tab', name: 'Live Preview', component: 'live' },
          { type: 'tab', name: 'Source', component: 'source' },
          { type: 'tab', name: 'WYSIWYG', component: 'wysiwyg' },
        ],
      },
    ],
  },
}

const loading = <CircularProgress size={28} sx={{ m: 2 }} />

// Each view binds to the session's shared document; views appear once it has loaded
function MarkdownView({ session, component }: { session: CollabSession; component: string }) {
  switch (component) {
    case 'live':
      return (
        <Suspense fallback={loading}>
          <LivePreviewView session={session} />
        </Suspense>
      )
    case 'source':
      return (
        <Suspense fallback={loading}>
          <SourceView session={session} />
        </Suspense>
      )
    case 'wysiwyg':
      return (
        <Suspense fallback={loading}>
          <WysiwygView session={session} />
        </Suspense>
      )
    default:
      return null
  }
}

function SessionLayout({ session }: { session: CollabSession }) {
  // One model per editor, unlike DevToolsTab's single module-level model
  const model = useMemo(() => Model.fromJson(json), [])
  const state = useCollabState(session)
  const factory = useCallback(
    (node: TabNode) => <MarkdownView session={session} component={node.getComponent() ?? ''} />,
    [session],
  )

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Box sx={{ position: 'relative', flexGrow: 1 }}>
        {state.loaded ? (
          <Layout model={model} factory={factory} />
        ) : (
          <Stack sx={{ height: '100%', alignItems: 'center', justifyContent: 'center' }}>
            <CircularProgress size={28} />
          </Stack>
        )}
      </Box>
      <MarkdownStatusBar state={state} />
    </Stack>
  )
}

// Tells the workbench context what this editor edits, its selected lines,
// and when the local user edits it
function useMarkdownSubject(tabId: string, object: MarkdownObject, session: CollabSession) {
  const state = useCollabState(session)
  const store = useWorkbenchContext()
  useWorkbenchSubject(
    tabId,
    { type: 'markdown', id: object.fileId, label: object.name, summary: object.helpText },
    { selection: state.selection ? { lines: state.selection } : undefined, dirty: state.saving },
  )
  useEffect(() => {
    let last = 0
    const observer = (_: Y.YTextEvent, transaction: Y.Transaction) => {
      // Edits from a view carry its binding as origin; seeding, remote updates and tool edits do not count
      if (!transaction.local || transaction.origin == null || transaction.origin === 'tool' || Date.now() - last < 5000) return
      last = Date.now()
      store.touch(tabId, 'edited')
    }
    session.ytext.observe(observer)
    return () => session.ytext.unobserve(observer)
  }, [session, store, tabId])
}

// Edits a markdown file with everyone who has it open: Live Preview, Source
// and WYSIWYG views of the same document, autosaved by the server-elected writer
function MarkdownEditor({ object, tabId }: EditorProps<MarkdownObject>) {
  const session = useCollabSession(object.fileId, object.readOnly)
  useMarkdownSubject(tabId, object, session)
  return <SessionLayout session={session} />
}

export default MarkdownEditor
