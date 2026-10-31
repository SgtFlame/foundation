// Obsidian's Live Preview: markdown syntax is hidden and rendered in place on
// every line the selection is not on, and revealed for editing on the lines
// it is on.

import { syntaxTree } from '@codemirror/language'
import type { EditorState, Range } from '@codemirror/state'
import {
  Decoration,
  type DecorationSet,
  EditorView,
  ViewPlugin,
  type ViewUpdate,
  WidgetType,
} from '@codemirror/view'
import type { SyntaxNodeRef } from '@lezer/common'

const hidden = Decoration.replace({})

class CheckboxWidget extends WidgetType {
  readonly checked: boolean

  constructor(checked: boolean) {
    super()
    this.checked = checked
  }

  eq(other: CheckboxWidget) {
    return other.checked === this.checked
  }

  toDOM(view: EditorView) {
    const input = document.createElement('input')
    input.type = 'checkbox'
    input.className = 'cm-lp-checkbox'
    input.checked = this.checked
    input.onmousedown = (event) => event.preventDefault()
    input.onclick = () => {
      // The marker is "[ ]" or "[x]"; toggle the character inside it
      const pos = view.posAtDOM(input) + 1
      view.dispatch({ changes: { from: pos, to: pos + 1, insert: this.checked ? ' ' : 'x' } })
    }
    return input
  }

  ignoreEvent() {
    return true
  }
}

class TextWidget extends WidgetType {
  readonly text: string
  readonly className: string

  constructor(text: string, className: string) {
    super()
    this.text = text
    this.className = className
  }

  eq(other: TextWidget) {
    return other.text === this.text && other.className === this.className
  }

  toDOM() {
    const span = document.createElement('span')
    span.className = this.className
    span.textContent = this.text
    return span
  }
}

class HorizontalRuleWidget extends WidgetType {
  eq() {
    return true
  }

  toDOM() {
    const hr = document.createElement('hr')
    hr.className = 'cm-lp-hr'
    return hr
  }
}

class ImageWidget extends WidgetType {
  readonly src: string
  readonly alt: string

  constructor(src: string, alt: string) {
    super()
    this.src = src
    this.alt = alt
  }

  eq(other: ImageWidget) {
    return other.src === this.src && other.alt === this.alt
  }

  toDOM() {
    const img = document.createElement('img')
    img.className = 'cm-lp-image'
    img.src = this.src
    img.alt = this.alt
    return img
  }
}

const calloutIcons: Record<string, string> = {
  note: '✎',
  info: 'ℹ',
  tip: '💡',
  success: '✔',
  question: '?',
  warning: '⚠',
  danger: '⚡',
  error: '✖',
  bug: '🐞',
  example: '☰',
  quote: '❝',
}

const calloutPattern = /^(\s*>\s*)\[!(\w+)\][+-]?(\s*)(.*)$/

// The line numbers the selection touches
function activeLines(state: EditorState): Set<number> {
  const lines = new Set<number>()
  for (const range of state.selection.ranges) {
    const last = state.doc.lineAt(range.to).number
    for (let n = state.doc.lineAt(range.from).number; n <= last; n++) lines.add(n)
  }
  return lines
}

function buildDecorations(view: EditorView, revealSelection: boolean): DecorationSet {
  const { state } = view
  const doc = state.doc
  const active = revealSelection ? activeLines(state) : new Set<number>()
  const isActive = (pos: number) => active.has(doc.lineAt(pos).number)
  const decorations: Range<Decoration>[] = []

  const lineClass = (from: number, to: number, className: string) => {
    for (let n = doc.lineAt(from).number, last = doc.lineAt(to).number; n <= last; n++) {
      decorations.push(Decoration.line({ class: className }).range(doc.line(n).from))
    }
  }

  // Hides a mark and, for block marks like "# " and "> ", the space after it
  const hide = (from: number, to: number, withSpace = false) => {
    if (withSpace && doc.sliceString(to, to + 1) === ' ') to++
    if (from < to) decorations.push(hidden.range(from, to))
  }

  const enter = (node: SyntaxNodeRef): boolean | void => {
    const name = node.name
    const heading = /^ATXHeading(\d)$/.exec(name)
    if (heading) {
      lineClass(node.from, node.from, `cm-lp-h${heading[1]}`)
      return
    }

    switch (name) {
      case 'HeaderMark':
        if (!isActive(node.from)) hide(node.from, node.to, true)
        return
      case 'EmphasisMark':
      case 'StrikethroughMark':
      case 'WikiLinkMark':
        if (!isActive(node.from)) hide(node.from, node.to)
        return
      case 'CodeMark':
        // Inline code backticks only; fences stay visible
        if (node.node.parent?.name === 'InlineCode' && !isActive(node.from)) hide(node.from, node.to)
        return
      case 'QuoteMark':
        if (!isActive(node.from)) hide(node.from, node.to, true)
        return
      case 'Blockquote': {
        const first = doc.lineAt(node.from)
        const callout = calloutPattern.exec(first.text)
        if (!callout) {
          lineClass(node.from, node.to, 'cm-lp-quote')
          return
        }
        const type = callout[2].toLowerCase()
        lineClass(node.from, node.to, `cm-lp-callout cm-lp-callout-${type}`)
        lineClass(first.from, first.from, 'cm-lp-callout-title')
        if (!isActive(first.from)) {
          // Replace "> [!type] " with the callout's icon, and its type when there is no title
          const start = first.from + callout[1].length
          const end = first.from + first.text.length - callout[4].length
          const label = `${calloutIcons[type] ?? calloutIcons.note} ${callout[4] ? '' : type[0].toUpperCase() + type.slice(1)}`
          decorations.push(Decoration.replace({ widget: new TextWidget(label, 'cm-lp-callout-label') }).range(start, end))
          hide(first.from, start)
        }
        return
      }
      case 'FencedCode':
        lineClass(node.from, node.to, 'cm-lp-codeblock')
        return
      case 'Link': {
        if (isActive(node.from)) return
        const marks = node.node.getChildren('LinkMark')
        if (marks.length < 2) return
        // [text](url "title"): hide everything but the text
        decorations.push(Decoration.mark({ class: 'cm-lp-link' }).range(marks[0].to, marks[1].from))
        hide(marks[0].from, marks[0].to)
        hide(marks[1].from, node.to)
        return false
      }
      case 'WikiLink': {
        if (isActive(node.from)) return
        // [[Target|alias]] shows only the alias
        const inner = doc.sliceString(node.from + 2, node.to - 2)
        const bar = inner.indexOf('|')
        if (bar >= 0) hide(node.from + 2, node.from + 3 + bar)
        decorations.push(Decoration.mark({ class: 'cm-lp-wikilink' }).range(node.from, node.to))
        return
      }
      case 'Tag':
        decorations.push(Decoration.mark({ class: 'cm-lp-tag' }).range(node.from, node.to))
        return
      case 'Image': {
        if (isActive(node.from)) return
        const url = node.node.getChild('URL')
        const marks = node.node.getChildren('LinkMark')
        if (!url || marks.length < 2) return
        const alt = doc.sliceString(marks[0].to, marks[1].from)
        decorations.push(
          Decoration.replace({ widget: new ImageWidget(doc.sliceString(url.from, url.to), alt) }).range(node.from, node.to),
        )
        return false
      }
      case 'HorizontalRule':
        if (!isActive(node.from)) {
          decorations.push(Decoration.replace({ widget: new HorizontalRuleWidget() }).range(node.from, node.to))
        }
        return
      case 'ListMark': {
        const item = node.node.parent
        if (item?.parent?.name !== 'BulletList' || isActive(node.from)) return
        if (item.getChild('Task')) hide(node.from, node.to, true)
        else decorations.push(Decoration.replace({ widget: new TextWidget('•', 'cm-lp-bullet') }).range(node.from, node.to))
        return
      }
      case 'TaskMarker':
        if (!isActive(node.from)) {
          const checked = doc.sliceString(node.from + 1, node.from + 2).toLowerCase() === 'x'
          decorations.push(Decoration.replace({ widget: new CheckboxWidget(checked) }).range(node.from, node.to))
          if (checked) {
            const line = doc.lineAt(node.to)
            if (node.to + 1 < line.to) {
              decorations.push(Decoration.mark({ class: 'cm-lp-task-done' }).range(node.to + 1, line.to))
            }
          }
        }
        return
    }
  }

  for (const { from, to } of view.visibleRanges) {
    syntaxTree(state).iterate({ from, to, enter })
  }
  return Decoration.set(decorations, true)
}

// revealSelection: false renders every line, for read-only text such as
// chat replies
export function livePreview({ revealSelection = true }: { revealSelection?: boolean } = {}) {
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet

      constructor(view: EditorView) {
        this.decorations = buildDecorations(view, revealSelection)
      }

      update(update: ViewUpdate) {
        if (
          update.docChanged ||
          update.selectionSet ||
          update.viewportChanged ||
          syntaxTree(update.startState) !== syntaxTree(update.state)
        ) {
          this.decorations = buildDecorations(update.view, revealSelection)
        }
      }
    },
    {
      decorations: (plugin) => plugin.decorations,
      eventHandlers: {
        // Ctrl/Cmd-click follows a rendered link
        mousedown(event, view) {
          const link = (event.target as HTMLElement).closest('.cm-lp-link')
          if (!link || !(event.ctrlKey || event.metaKey)) return false
          let node = syntaxTree(view.state).resolveInner(view.posAtDOM(link), 1)
          while (node.name !== 'Link' && node.parent) node = node.parent
          const url = node.getChild('URL')
          if (!url) return false
          window.open(view.state.sliceDoc(url.from, url.to), '_blank', 'noopener')
          return true
        },
      },
    },
  )
}
