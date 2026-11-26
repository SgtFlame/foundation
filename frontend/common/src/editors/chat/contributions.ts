import BugReportIcon from '@mui/icons-material/BugReport'
import ChatIcon from '@mui/icons-material/Chat'
import { lazy } from 'react'
import { ChatObject } from './ChatObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import DebugControl from './DebugControl.tsx'
import { chatCommands, chatContextSettings } from './points.ts'
import { matchMode, modeList, modeMatchMessage } from '../../components/chat/modeMatch.ts'

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

// /mode <name> switches to an offered mode by its label or id (case, emoji
// and spaces ignored, a unique prefix enough); /mode alone opens the list
// (forge 15e)
extensions.contribute(chatCommands, {
  id: 'mode',
  source: 'chat',
  order: 300,
  description: 'Switch the chat mode, list the modes, or make a new one (/mode new)',
  usage: '[name | new]',
  run: (arg, ctx) => {
    const query = arg.trim()
    if (!query) {
      if (!ctx.pickMode()) ctx.say(`The modes here are:\n\n${modeList(ctx.modes)}`)
      return
    }
    // /mode new makes a mode of your own (forge 15h)
    if (query.toLowerCase() === 'new') {
      if (!ctx.newMode()) ctx.say('There is no mode here that makes new modes.')
      return
    }
    const match = matchMode(query, ctx.modes, ctx.allModes)
    if (match.kind === 'found') ctx.setMode(match.mode.id)
    ctx.say(modeMatchMessage(query, match, ctx.modes, ctx.kindName))
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
