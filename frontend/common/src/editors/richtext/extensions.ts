// The rich-text editor's extensions (forge 35b-1): TipTap 2's MIT core and
// extensions only (iteration 35's question 5: TipTap's paid features are
// built on this core). The schema is what a document file holds, so the
// room seeds and saves with the same one the editor edits.

import { getSchema, type AnyExtension } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import Underline from '@tiptap/extension-underline'
import Link from '@tiptap/extension-link'
import Table from '@tiptap/extension-table'
import TableRow from '@tiptap/extension-table-row'
import TableCell from '@tiptap/extension-table-cell'
import TableHeader from '@tiptap/extension-table-header'
import Image from '@tiptap/extension-image'
import TaskList from '@tiptap/extension-task-list'
import TaskItem from '@tiptap/extension-task-item'
import TextAlign from '@tiptap/extension-text-align'
import Highlight from '@tiptap/extension-highlight'
import TextStyle from '@tiptap/extension-text-style'
import Color from '@tiptap/extension-color'
import Subscript from '@tiptap/extension-subscript'
import Superscript from '@tiptap/extension-superscript'
import CharacterCount from '@tiptap/extension-character-count'
import type { Schema } from '@tiptap/pm/model'
import { Footnote, MathBlock, MathInline, Mention } from './nodes.ts'
import { Indent, PageBreak, PageNumber, SectionBreak, TableOfContents } from './pageNodes.ts'
import { blockNodes } from './blockNodes.ts'

// Every extension but collaboration's, which needs the room
export function baseExtensions(): AnyExtension[] {
  return [
    // History is the room's (Collaboration's Yjs undo), never TipTap's own
    StarterKit.configure({ history: false }),
    Underline,
    Link.configure({ openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer', target: null } }),
    Table.configure({ resizable: true }),
    TableRow,
    TableHeader,
    TableCell,
    Image.configure({ inline: false }),
    TaskList,
    TaskItem.configure({ nested: true }),
    TextAlign.configure({ types: ['heading', 'paragraph'] }),
    Highlight.configure({ multicolor: true }),
    TextStyle,
    Color,
    Subscript,
    Superscript,
    CharacterCount,
    Footnote,
    MathInline,
    MathBlock,
    Mention,
    PageBreak,
    SectionBreak,
    TableOfContents,
    PageNumber,
    Indent,
    ...blockNodes,
  ]
}

let schema: Schema | undefined

// The schema of a document file
export function richTextSchema(): Schema {
  return (schema ??= getSchema(baseExtensions()))
}
