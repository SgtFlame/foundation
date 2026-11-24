// A person's recent activity (forge 15c, documentation/Activity.md): what
// they did by hand, grouped into gestures, readable by them alone with
// useQuery<Activity>('activity.mine', {after?, limit?, sendable?}). The
// calls record a focused tab, dismiss gestures (never sent to a model)
// and clear the history.

import { live, newId } from '../live/LiveConnection.ts'

// What a gesture was about: a workbench subject or a page's
export interface ActivityThing {
  type: string
  id: string
  label?: string
  details?: Record<string, string>
}

// A field a command changed, with its new value unless that value is
// content the history never copies (omitted)
export interface ActivityField {
  id: string
  name: string
  value?: unknown
  omitted?: boolean
}

export type ActivityKind = 'focus' | 'ui' | 'command' | 'op'

export interface ActivityEntry {
  id: string
  kind: ActivityKind
  request?: string
  time: string
  app?: string
  subject?: ActivityThing
  // ui
  page?: string
  title?: string
  handler?: string
  event?: string
  path?: string[]
  node?: string
  label?: string
  // command, op
  command?: string
  commandTitle?: string
  entity?: string
  entityName?: string
  target?: string
  recordTitle?: string
  change?: string
  fields?: ActivityField[]
  summary?: string
}

// One request's entries: a click and the commands it caused
export interface ActivityGesture {
  id: string
  start: string
  time: string
  app?: string
  subject?: ActivityThing
  summary: string
  repeats?: number
  dismissed?: boolean
  entries: ActivityEntry[]
}

export interface Activity {
  recording: boolean
  gestures: ActivityGesture[] // newest last
}

export interface ActivityParams {
  after?: string // RFC 3339
  limit?: number
  sendable?: boolean
}

function run(call: string, args: unknown): Promise<void> {
  return live.call(call, args, () => {})
}

export const ActivityCalls = {
  // Records that this person focused a tab showing subject
  focus: (subject: ActivityThing) => run('activity.focus', { id: newId('focus'), subject }),
  // Marks gestures so they are never sent to a model
  dismiss: (ids: string[]) => run('activity.dismiss', { ids }),
  // Deletes everything kept
  clear: () => run('activity.clear', {}),
}
