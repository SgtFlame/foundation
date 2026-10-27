// Tools in the browser (documentation/Tools.md, WorkbenchContext.md):
// the conversation's Tools switch and the client tools the model may call
import BuildIcon from '@mui/icons-material/Build'
import { lazy } from 'react'
import { chatClientTools, chatContextSettings } from '../editors/chat/points.ts'
import { ChronicleObject } from '../editors/chronicle/ChronicleObject.ts'
import { openMarkdownAt } from '../editors/markdown/openMarkdown.ts'
import { extensions } from '../workbench/extensions/index.ts'
import ToolsControl from './ToolsControl.tsx'

extensions.contribute(chatContextSettings, {
  id: 'tools',
  source: 'tools',
  order: 300,
  title: 'Tools',
  icon: BuildIcon,
  Control: ToolsControl,
})

extensions.contribute(chatClientTools, {
  id: 'workbench.getState',
  source: 'tools',
  order: 100,
  title: 'Look at the workbench',
  description:
    'Returns what the user has open in their workbench: every editor tab with what it edits and its selection, the focus order, and the subjects recently viewed or edited. Give a tab to get only that editor. Use it when the workspace summary you were given is not enough.',
  input: { type: 'object', properties: { tab: { type: 'string', description: 'An editor tab, e.g. markdown:123' } } },
  effect: 'read',
  run: (args, { store, tabId }) => {
    const state = store.getState()
    if (typeof args.tab === 'string' && args.tab) {
      const editor = state.editors.find((e) => e.tab === args.tab)
      if (!editor) throw new Error(`there is no open tab ${args.tab}`)
      return { editor, focused: state.focused === editor.tab }
    }
    return { chatTab: tabId, editors: state.editors, focused: state.focused, focusOrder: state.focusOrder.slice(0, 10), recent: state.recent }
  },
})

extensions.contribute(chatClientTools, {
  id: 'workbench.open',
  source: 'tools',
  order: 110,
  title: 'Open in the workbench',
  description:
    'Shows something to the user in their workbench: focuses an open editor tab, opens a Volume file (optionally selecting lines), or opens a Chronicle entry. Give exactly one of tab, path or chronicleId.',
  input: {
    type: 'object',
    properties: {
      tab: { type: 'string', description: 'An open editor tab to focus, from the workspace or workbench.getState' },
      path: { type: 'string', description: 'A Volume file, e.g. veritas/notes.md' },
      startLine: { type: 'integer', minimum: 1 },
      endLine: { type: 'integer', minimum: 1 },
      chronicleId: { type: 'string', description: 'A Chronicle entry id' },
      title: { type: 'string', description: "The entry's title, for its tab" },
    },
  },
  effect: 'ui',
  run: async (args, { registry }) => {
    if (typeof args.tab === 'string' && args.tab) {
      if (!registry.focus(args.tab)) throw new Error(`there is no open tab ${args.tab}`)
      return { focused: args.tab }
    }
    if (typeof args.path === 'string' && args.path) {
      const start = typeof args.startLine === 'number' ? args.startLine : undefined
      const end = typeof args.endLine === 'number' ? args.endLine : start
      await openMarkdownAt(registry, args.path, start ? [start, end ?? start] : undefined)
      return { opened: args.path }
    }
    if (typeof args.chronicleId === 'string' && args.chronicleId) {
      registry.edit(new ChronicleObject({ id: args.chronicleId, title: typeof args.title === 'string' ? args.title : 'Chronicle' }))
      return { opened: args.chronicleId }
    }
    throw new Error('give one of tab, path or chronicleId')
  },
})

extensions.contribute(chatClientTools, {
  id: 'chat.askChoice',
  source: 'tools',
  order: 120,
  title: 'Ask you to choose',
  description:
    'Asks the user to pick one of a few candidates, shown as buttons, when what they mean is ambiguous (two people or files could be meant). They may type something else instead. Returns the choice (with its value and subject) or their text.',
  input: {
    type: 'object',
    properties: {
      question: { type: 'string', minLength: 1 },
      choices: {
        type: 'array',
        minItems: 2,
        maxItems: 8,
        items: {
          type: 'object',
          properties: {
            label: { type: 'string', minLength: 1 },
            value: { type: 'string', description: 'What to hand back, such as an id; the label when absent' },
            subject: { type: 'object', description: 'A workbench subject, {type, id, label}' },
          },
          required: ['label'],
        },
      },
    },
    required: ['question', 'choices'],
  },
  effect: 'ui',
  Card: lazy(() => import('./AskChoiceCard.tsx')),
})
