// A document room's content (forge 35b-1): the shared document is a
// Y.XmlFragment that y-prosemirror keeps in step with the editor, and the
// file's text is the document file's envelope {format: 1, doc}, the same
// for a Volume's document file and a record's richText field.

import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from 'y-prosemirror'
import type * as Y from 'yjs'
import { log } from '../../logging/index.ts'
import type { CollabContent, ContentFactory } from '../markdown/collab/CollabSession.ts'
import { richTextSchema } from './extensions.ts'

const logger = log.In('collab').Tags('richtext')

// The fragment the editor binds to
export const fragmentName = 'prosemirror'

export function fragmentOf(doc: Y.Doc): Y.XmlFragment {
  return doc.getXmlFragment(fragmentName)
}

// The doc in a document file's text: the envelope, or a bare doc
export function docOf(text: string): Record<string, unknown> | undefined {
  try {
    const parsed = JSON.parse(text) as { format?: number; doc?: Record<string, unknown>; type?: string }
    if (parsed.format === 1 && parsed.doc?.type === 'doc') return parsed.doc
    if (parsed.type === 'doc') return parsed as Record<string, unknown>
  } catch {
    // not JSON: an empty document
  }
  return undefined
}

export const richTextContent: ContentFactory = (doc): CollabContent => {
  const fragment = fragmentOf(doc)
  return {
    seed(text) {
      const json = docOf(text)
      if (!json) return true
      try {
        prosemirrorJSONToYXmlFragment(richTextSchema(), json, fragment)
        return true
      } catch (e) {
        // A node the schema lacks: never seed (and so never save) less than the file holds
        logger.fromError(e as Error).warn('cannot load the document')
        return false
      }
    },
    text: () => JSON.stringify({ format: 1, doc: yXmlFragmentToProsemirrorJSON(fragment) }),
    edits: false,
    bridgeCursors: false,
  }
}
