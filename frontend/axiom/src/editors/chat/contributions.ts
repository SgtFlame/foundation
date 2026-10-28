import BugReportIcon from '@mui/icons-material/BugReport'
import ChatIcon from '@mui/icons-material/Chat'
import { lazy } from 'react'
import { ChatObject } from './ChatObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import DebugControl from './DebugControl.tsx'
import { chatCommands, chatContextSettings } from './points.ts'

extensions.contribute(editors, {
  id: 'chat',
  source: 'chat',
  name: 'Chat',
  component: lazy(() => import('./ChatEditor.tsx')),
})

extensions.contribute(chatContextSettings, {
  id: 'debug',
  source: 'chat',
  order: 900,
  title: 'Debug',
  icon: BugReportIcon,
  Control: DebugControl,
})

extensions.contribute(chatCommands, {
  id: 'new',
  source: 'chat',
  order: 100,
  description: 'Start a new conversation',
  run: (_, ctx) => ctx.newConversation(),
})

extensions.contribute(chatCommands, {
  id: 'level',
  source: 'chat',
  order: 200,
  description: 'Switch the level for the next messages',
  usage: '<level>',
  run: (arg, ctx) => {
    if (!ctx.setLevel(arg.trim())) {
      ctx.say(`There is no level \`${arg.trim()}\`. The levels are ${ctx.levels.map((l) => `\`${l.id}\` (${l.label})`).join(', ')}.`)
    }
  },
})

extensions.contribute(chatCommands, {
  id: 'help',
  source: 'chat',
  order: 900,
  description: 'List the commands',
  run: (_, ctx) =>
    ctx.say(
      ctx.commands.map((c) => `- \`/${c.id}${c.usage ? ' ' + c.usage : ''}\`: ${c.description}`).join('\n') +
        '\n\nEnter sends, Shift+Enter adds a line.',
    ),
})

extensions.contribute(commands, {
  id: 'open-chat',
  source: 'chat',
  order: 100,
  title: 'Chat',
  icon: ChatIcon,
  section: 'AgentOS',
  run: ({ registry }) => registry.edit(new ChatObject()),
})
