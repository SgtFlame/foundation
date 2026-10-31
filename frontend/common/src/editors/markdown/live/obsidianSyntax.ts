// Obsidian's inline syntax for @lezer/markdown: [[wikilinks]] and #tags.
// Callouts (> [!note]) are ordinary blockquotes, recognised by livePreview.

import { tags } from '@lezer/highlight'
import type { MarkdownConfig } from '@lezer/markdown'

const openBracket = 91 // [
const hash = 35 // #

// [[Target]] or [[Target|alias]], on one line
export const WikiLink: MarkdownConfig = {
  defineNodes: [
    { name: 'WikiLink', style: tags.link },
    { name: 'WikiLinkMark', style: tags.processingInstruction },
  ],
  parseInline: [
    {
      name: 'WikiLink',
      before: 'Link',
      parse(cx, next, pos) {
        if (next !== openBracket || cx.char(pos + 1) !== openBracket) return -1
        const rest = cx.slice(pos + 2, cx.end)
        const close = rest.indexOf(']]')
        if (close <= 0 || rest.slice(0, close).includes('\n')) return -1
        const end = pos + 2 + close
        return cx.addElement(
          cx.elt('WikiLink', pos, end + 2, [
            cx.elt('WikiLinkMark', pos, pos + 2),
            cx.elt('WikiLinkMark', end, end + 2),
          ]),
        )
      },
    },
  ],
}

// #tag after whitespace or at the start of the text; not all digits
const tagPattern = /^#[\p{L}\p{N}_/-]*[\p{L}_/-][\p{L}\p{N}_/-]*/u

export const Tag: MarkdownConfig = {
  defineNodes: [{ name: 'Tag', style: tags.labelName }],
  parseInline: [
    {
      name: 'Tag',
      parse(cx, next, pos) {
        if (next !== hash) return -1
        if (pos > cx.offset && !/\s/.test(cx.slice(pos - 1, pos))) return -1
        const match = tagPattern.exec(cx.slice(pos, cx.end))
        return match ? cx.addElement(cx.elt('Tag', pos, pos + match[0].length)) : -1
      },
    },
  ],
}
