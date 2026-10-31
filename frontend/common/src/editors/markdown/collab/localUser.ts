// The name and colour other editors see for this browser, until users log in

export interface LocalUser {
  name: string
  color: string
}

const storageKey = 'axiom.collab.user'

const colors = ['#e53935', '#8e24aa', '#3949ab', '#039be5', '#00897b', '#7cb342', '#fb8c00', '#6d4c41']

let cached: LocalUser | undefined

export function localUser(): LocalUser {
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
