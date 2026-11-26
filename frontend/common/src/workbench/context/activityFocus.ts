// Tells the server's activity history (forge 15c, documentation/Activity.md)
// which subject the person focused: only when the focused subject changes,
// and only once it has stayed focused for a moment, so flicking through
// tabs records the one they stopped on.

import { ActivityCalls } from '../../services/Activity.ts'
import { log } from '../../logging/index.ts'
import { workbenchContext } from './WorkbenchContextStore.ts'

const settle = 1500 // ms a subject must stay focused

// Starts following the focus; the returned function stops it
export function startActivityFocus(): () => void {
  let last = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  const check = () => {
    const subject = workbenchContext.focusedSubject()
    const key = subject ? `${subject.type}\u0000${subject.id}` : ''
    if (timer) clearTimeout(timer)
    if (!subject || key === last) return
    timer = setTimeout(() => {
      last = key
      const { type, id, label, details } = subject
      ActivityCalls.focus({ type, id, label, details }).catch((err: unknown) => {
        log.In('activity').fromError(err).debug('could not record a focused tab')
      })
    }, settle)
  }
  const unsubscribe = workbenchContext.subscribe(check)
  return () => {
    unsubscribe()
    if (timer) clearTimeout(timer)
  }
}
