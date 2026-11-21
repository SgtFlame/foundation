// The name and colour other editors see for this browser: the signed-in
// user's name and presence colour, else a guest's kept in this browser
import { live } from '../../../live/LiveConnection.ts'
import { session } from '../../../session/session.ts'

export interface LocalUser {
  name: string
  color: string
}

const storageKey = 'axiom.collab.user'

const colors = ['#e53935', '#8e24aa', '#3949ab', '#039be5', '#00897b', '#7cb342', '#fb8c00', '#6d4c41']

let cached: LocalUser | undefined

export function localUser(): LocalUser {
  const user = session.get().account?.user
  if (user) return { name: user.displayName || user.username, color: live.getHello()?.color ?? guest().color }
  return guest()
}

function guest(): LocalUser {
  if (cached) return cached
  try {
    const stored = localStorage.getItem(storageKey)
    if (stored) return (cached = JSON.parse(stored) as LocalUser)
  } catch {
    // storage unavailable or corrupt: make a new user
  }
  cached = {
    name: `Guest ${Math.floor(1000 + Math.random() * 9000)}`,
    color: colors[Math.floor(Math.random() * colors.length)],
  }
  try {
    localStorage.setItem(storageKey, JSON.stringify(cached))
  } catch {
    // keep the user for this page only
  }
  return cached
}
