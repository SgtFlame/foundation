// The avatars of everyone else in an object, for an editor's toolbar: one
// per user, in their colour, their windows' count in the tooltip.

import { Avatar, AvatarGroup, Tooltip } from '@mui/material'
import type { PresentUser } from './usePresence.ts'

export interface PresenceAvatarsProps {
  users: PresentUser[]
  max?: number
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/)
  return ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : '')).toUpperCase()
}

export function PresenceAvatars({ users, max = 5 }: PresenceAvatarsProps) {
  const byUser = new Map<string, { user: PresentUser; windows: number }>()
  for (const u of users) {
    const e = byUser.get(u.userId)
    if (e) e.windows++
    else byUser.set(u.userId, { user: u, windows: 1 })
  }
  if (byUser.size === 0) return null
  return (
    <AvatarGroup max={max} sx={{ '& .MuiAvatar-root': { width: 24, height: 24, fontSize: 11 } }}>
      {[...byUser.values()].map(({ user, windows }) => (
        <Tooltip key={user.userId} title={windows > 1 ? `${user.name} (${windows} windows)` : user.name}>
          <Avatar alt={user.name} sx={{ bgcolor: user.color }}>
            {initials(user.name)}
          </Avatar>
        </Tooltip>
      ))}
    </AvatarGroup>
  )
}

export default PresenceAvatars
