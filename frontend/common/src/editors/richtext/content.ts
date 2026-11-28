// A document room's content (forge 35b-1): the shared document is a
// Y.XmlFragment that y-prosemirror keeps in step with the editor, and the
// file's text is the document file's envelope {format: 1, doc}, the same
// for a Volume's document file and a record's richText field.
//
// Page view (forge 35b-2) keeps the doc node's attrs beside it in the same
// Y.Doc, since y-prosemirror syncs no attrs of the doc node: the page setup
// in the Y.Map `page` (a key per stored field) and each header and footer,
// a small document of its own, in a Y.XmlFragment named after it. The file
// holds them as doc.attrs {page, header, footer, firstHeader, firstFooter},
// so they travel with it and the derived Markdown ignores them.

import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from 'y-prosemirror'
import type * as Y from 'yjs'
import { log } from '../../logging/index.ts'
import type { CollabContent, ContentFactory } from '../markdown/collab/CollabSession.ts'
import { richTextSchema } from './extensions.ts'
import { setupOf, storedSetup, type PageSetup } from './pages.ts'

const logger = log.In('collab').Tags('richtext')

// The fragment the editor binds to
export const fragmentName = 'prosemirror'

export function fragmentOf(doc: Y.Doc): Y.XmlFragment {
  return doc.getXmlFragment(fragmentName)
}

export const headerFooters = ['header', 'footer', 'firstHeader', 'firstFooter'] as const
export type HeaderFooter = (typeof headerFooters)[number]

export function pageMapOf(doc: Y.Doc): Y.Map<unknown> {
  return doc.getMap('page')
}

export function headerFooterOf(doc: Y.Doc, name: HeaderFooter): Y.XmlFragment {
  return doc.getXmlFragment(name)
}

// The room's page setup
export function pageSetupOf(doc: Y.Doc): PageSetup {
  return setupOf(pageMapOf(doc).toJSON())
}

// Changes the room's page setup, one key per stored field, so two people
// changing different fields both keep their change
export function setPageSetup(doc: Y.Doc, setup: PageSetup) {
  const map = pageMapOf(doc)
  const stored = storedSetup(setup)
  doc.transact(() => {
    for (const key of [...map.keys()]) if (!(key in stored)) map.delete(key)
    for (const [key, value] of Object.entries(stored)) {
      if (JSON.stringify(map.get(key)) !== JSON.stringify(value)) map.set(key, value)
    }
  })
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
        const { attrs, ...body } = json as { attrs?: Record<string, unknown> }
        prosemirrorJSONToYXmlFragment(richTextSchema(), body, fragment)
        if (attrs && typeof attrs === 'object') {
          const page = attrs.page
          if (page && typeof page === 'object') for (const [k, v] of Object.entries(storedSetup(setupOf(page)))) pageMapOf(doc).set(k, v)
          for (const name of headerFooters) {
            const sub = attrs[name] as { type?: string } | undefined
            if (sub?.type === 'doc') prosemirrorJSONToYXmlFragment(richTextSchema(), sub, headerFooterOf(doc, name))
          }
        }
        return true
      } catch (e) {
        // A node the schema lacks: never seed (and so never save) less than the file holds
        logger.fromError(e as Error).warn('cannot load the document')
        return false
      }
    },
    text: () => {
      const attrs: Record<string, unknown> = {}
      const page = pageMapOf(doc).toJSON()
      if (Object.keys(page).length > 0) attrs.page = page
      for (const name of headerFooters) {
        const sub = headerFooterOf(doc, name)
        if (sub.length > 0) attrs[name] = yXmlFragmentToProsemirrorJSON(sub)
      }
      const body = yXmlFragmentToProsemirrorJSON(fragment)
      return JSON.stringify({ format: 1, doc: Object.keys(attrs).length > 0 ? { type: 'doc', attrs, content: body.content } : body })
    },
    edits: false,
    bridgeCursors: false,
  }
}
