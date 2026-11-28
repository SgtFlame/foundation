// Edits a Volume's document file with everyone who has it open (forge
// 35b-1): the rich-text editor in page mode over the file's room, its
// selection told to the workbench as the top-level block it starts in.
// The document's page setup says whether it opens in block mode (forge
// 35c-2), which the toolbar switches for everyone.

import { useEffect, useState } from 'react'
import { useWorkbenchContext, useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import { openWorkbenchLink } from '../../workbench/links.ts'
import { useCollabSession, useCollabState } from '../markdown/collab/useCollabSession.ts'
import { richTextContent, fragmentOf, setPageSetup } from './content.ts'
import { usePageSetup } from './PageView.tsx'
import type { RichTextObject } from './RichTextObject.ts'
import { RichTextView, type RichTextSelection } from './RichTextView.tsx'

function RichTextEditor({ object, tabId, registry }: EditorProps<RichTextObject>) {
  const session = useCollabSession(object.fileId, object.readOnly, richTextContent)
  const state = useCollabState(session)
  const store = useWorkbenchContext()
  const [selection, setSelection] = useState<RichTextSelection>()
  const setup = usePageSetup(session.doc)
  useWorkbenchSubject(
    tabId,
    { type: 'document', id: object.fileId, label: object.name, summary: object.helpText },
    {
      selection: selection && selection.blocks.length ? { node: String(selection.blocks[0]), part: selection.heading || undefined } : undefined,
      dirty: state.saving || !!state.pending,
    },
  )
  // The local user's edits mark the subject edited, at most every 5 seconds
  useEffect(() => {
    let last = 0
    const fragment = fragmentOf(session.doc)
    const observer = (_: unknown, tx: { local: boolean; origin: unknown }) => {
      if (!tx.local || tx.origin == null || Date.now() - last < 5000) return
      last = Date.now()
      store.touch(tabId, 'edited')
    }
    fragment.observeDeep(observer)
    return () => fragment.unobserveDeep(observer)
  }, [session, store, tabId])

  return (
    <RichTextView session={session} readOnly={object.readOnly} mode={setup.blocks ? 'blocks' : 'page'} label={object.name} onSelection={setSelection}
      onMode={(mode) => setPageSetup(session.doc, { ...setup, blocks: mode === 'blocks' })}
      onLink={(href) => (href.startsWith('workbench:') ? openWorkbenchLink(registry, href) : false)} />
  )
}

export default RichTextEditor
