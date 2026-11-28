// doc.blocks (forge 35c-2): a record's richText field in block mode, the
// Notion-style view of the same document doc.richText edits: the slash menu
// (the built-in items and the items prop), drag handles, toggles, callouts,
// columns and page links. A command item raises command {id, block} for the
// page's handler graph.

import type { RendererProps } from '../../points.ts'
import DocRichText from './DocRichText.tsx'

export default function DocBlocks(p: RendererProps) {
  return <DocRichText {...p} props={{ ...p.props, mode: 'blocks' }} />
}
