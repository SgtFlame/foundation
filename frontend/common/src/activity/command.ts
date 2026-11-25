// /activity: the person's recent activity from the chat (forge 15d), for
// those who want finer control than the ⚙ switch. Two things differ:
// whether this conversation sends what they did (its activity context
// setting), and whether anything is recorded at all (their activity
// settings section, user scope, which also says how long and how much
// is kept).

import { Config } from '../services/Config.ts'
import { ActivityCalls } from '../services/Activity.ts'
import type { ChatCommandContext } from '../editors/chat/points.ts'

export const activityUsage = '[on|off|record on|record off|clear|keep <hours>h|keep <count>|since <minutes>m]'

// What /activity was asked to do
export type ActivityCommand =
  | { kind: 'status' }
  | { kind: 'send'; on: boolean }
  | { kind: 'record'; on: boolean }
  | { kind: 'clear' }
  | { kind: 'keepHours'; hours: number }
  | { kind: 'keepCount'; count: number }
  | { kind: 'since'; minutes: number }
  | { kind: 'invalid'; why: string }

// The settings' limits (Foundation's activity section and provider)
const limits = { hours: [1, 24], count: [10, 1000], minutes: [1, 1440] } as const

function within(n: number, [lo, hi]: readonly [number, number], what: string): string | undefined {
  return Number.isInteger(n) && n >= lo && n <= hi ? undefined : `${what} is from ${lo} to ${hi}.`
}

// Reads /activity's argument
export function parseActivityCommand(arg: string): ActivityCommand {
  const words = arg.trim().toLowerCase().split(/\s+/).filter(Boolean)
  const [first, second] = words
  const onOff = (w?: string) => (w === 'on' ? true : w === 'off' ? false : undefined)
  if (words.length === 0 || (first === 'status' && words.length === 1)) return { kind: 'status' }
  if (words.length === 1 && onOff(first) !== undefined) return { kind: 'send', on: onOff(first)! }
  if (first === 'record' && words.length === 2 && onOff(second) !== undefined) return { kind: 'record', on: onOff(second)! }
  if (first === 'clear' && words.length === 1) return { kind: 'clear' }
  if (first === 'keep' && words.length === 2) {
    const hours = /^(\d+)h$/.exec(second)
    if (hours) {
      const n = Number(hours[1])
      const why = within(n, limits.hours, 'How long it is kept')
      return why ? { kind: 'invalid', why } : { kind: 'keepHours', hours: n }
    }
    if (/^\d+$/.test(second)) {
      const n = Number(second)
      const why = within(n, limits.count, 'How many entries are kept')
      return why ? { kind: 'invalid', why } : { kind: 'keepCount', count: n }
    }
  }
  if (first === 'since' && words.length === 2) {
    const m = /^(\d+)m?$/.exec(second)
    if (m) {
      const n = Number(m[1])
      const why = within(n, limits.minutes, 'How far a first message looks back')
      return why ? { kind: 'invalid', why } : { kind: 'since', minutes: n }
    }
  }
  return { kind: 'invalid', why: `I don't know \`/activity ${arg.trim()}\`.` }
}

interface Recording {
  record?: boolean
  keepHours?: number
  keepCount?: number
}

// The person's own activity settings, and a change to them
async function recording(): Promise<{ value: Recording; revision: number }> {
  const view = await Config.get({ section: 'activity', scope: 'user' })
  return { value: (view.value ?? {}) as Recording, revision: view.revision }
}

async function setRecording(patch: Recording): Promise<void> {
  const { revision } = await recording()
  const res = await Config.set({ section: 'activity', scope: 'user', patch: patch as Record<string, unknown>, revision })
  if (res.errors?.length) throw new Error(res.errors.map((e) => `${e.path}: ${e.message}`).join('; '))
}

const help = [
  '- `/activity`: what is recorded and sent',
  '- `/activity on` or `off`: send what you did with this conversation\'s messages, or not',
  '- `/activity record on` or `off`: record what you do at all, in every conversation (off also deletes what is kept)',
  '- `/activity clear`: delete what is kept now',
  '- `/activity keep 4h` or `keep 300`: how long, and how many entries, are kept',
  '- `/activity since 30m`: how far this conversation\'s first message looks back',
].join('\n')

// Runs /activity, saying what it did
export async function runActivityCommand(arg: string, ctx: ChatCommandContext): Promise<void> {
  const cmd = parseActivityCommand(arg)
  const conversation = ctx.contextSettings.activity ?? {}
  const sending = conversation.enabled !== false
  switch (cmd.kind) {
    case 'invalid':
      ctx.say(`${cmd.why}\n\n${help}`)
      return
    case 'status': {
      const { value } = await recording()
      const since = Number(conversation.lookBackMinutes) || 30
      ctx.say(
        (value.record === false
          ? '**Recording is off**: nothing you do is kept, so nothing is sent.'
          : `**Recording is on**: what you do by hand is kept ${value.keepHours ?? 4} hours, at most ${value.keepCount ?? 300} entries, and only you can read it.`) +
          `\n\nThis conversation ${sending ? '**sends**' : '**does not send**'} what you did since your last message (a first message looks back ${since} minutes).\n\n${help}`,
      )
      return
    }
    case 'send':
      ctx.setContextSetting('activity', { ...conversation, enabled: cmd.on })
      ctx.say(cmd.on ? 'This conversation now sends what you did since your last message.' : 'This conversation no longer sends what you did. It is still recorded: `/activity record off` stops that.')
      return
    case 'since':
      ctx.setContextSetting('activity', { ...conversation, lookBackMinutes: cmd.minutes })
      ctx.say(`A first message in this conversation now looks back ${cmd.minutes} minutes.`)
      return
    case 'record':
      await setRecording({ record: cmd.on })
      ctx.say(cmd.on ? 'Recording is on again: what you do by hand from now on is kept.' : 'Recording is off, in every conversation: what was kept is deleted and nothing more is kept. `/activity record on` turns it back on.')
      return
    case 'clear':
      await ActivityCalls.clear()
      ctx.say('Your recent activity is deleted. Recording goes on: `/activity record off` stops it.')
      return
    case 'keepHours':
      await setRecording({ keepHours: cmd.hours })
      ctx.say(`What you do is now kept ${cmd.hours} hours.`)
      return
    case 'keepCount':
      await setRecording({ keepCount: cmd.count })
      ctx.say(`At most ${cmd.count} entries of what you do are now kept.`)
      return
  }
}
