import HistoryIcon from '@mui/icons-material/History'
import { chatCommands, chatContextSettings } from '../editors/chat/points.ts'
import { log } from '../logging/index.ts'
import { activityUsage, runActivityCommand } from './command.ts'
import { extensions } from '../workbench/extensions/index.ts'
import ActivityControl from './ActivityControl.tsx'

// The composer's switch for the person's recent activity, beside "Send
// what I have open" (forge 15d)
extensions.contribute(chatContextSettings, {
  id: 'activity',
  source: 'activity',
  order: 210,
  title: 'Recent activity',
  icon: HistoryIcon,
  Control: ActivityControl,
})

// /activity: finer control from the chat: this conversation's sending,
// recording at all, clearing, how much is kept and how far back a first
// message looks (forge 15d)
extensions.contribute(chatCommands, {
  id: 'activity',
  source: 'activity',
  order: 300,
  description: 'What you did by hand: send it or not here, record it or not at all, clear it, keep more or less',
  usage: activityUsage,
  run: (arg, ctx) => {
    runActivityCommand(arg, ctx).catch((err: unknown) => {
      log.In('activity').fromError(err).warn('/activity %s failed', arg)
      ctx.say(`\`/activity ${arg.trim()}\` failed: ${err instanceof Error ? err.message : String(err)}`)
    })
  },
})
