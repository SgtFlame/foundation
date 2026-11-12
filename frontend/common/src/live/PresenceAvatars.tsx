// The avatars of everyone else in an object, for an editor's toolbar: one
// per user, in their colour, their windows' count in the tooltip. A user's
// chat working as the Builder (forge 06g) is an avatar of its own: a robot
// badged with the user's initials.

import { Avatar, AvatarGroup, Badge, Tooltip } from '@mui/material'
import SmartToyOutlined from '@mui/icons-material/SmartToyOutlined'
import type { PresentUser } from './usePresence.ts'

export interface PresenceAvatarsProps {
  users: PresentUser[]
  max?: number
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/)
  return ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : '')).toUpperCase()
}

const actorNames: Record<string, string> = { builder: 'Builder' }

// How a present connection is named: the user, or their actor for them
export function presentName(u: Pick<PresentUser, 'name' | 'actor'>): string {
  if (!u.actor) return u.name
  return `${actorNames[u.actor] ?? u.actor}, for ${u.name}`
}

export function PresenceAvatars({ users, max = 5 }: PresenceAvatarsProps) {
  const byUser = new Map<string, { user: PresentUser; windows: number }>()
  for (const u of users) {
    const key = `${u.userId}/${u.actor ?? ''}`
    const e = byUser.get(key)
    if (e) e.windows++
    else byUser.set(key, { user: u, windows: 1 })
  }
  if (byUser.size === 0) return null
  return (
    <AvatarGroup max={max} sx={{ '& .MuiAvatar-root': { width: 24, height: 24, fontSize: 11 } }}>
      {[...byUser.entries()].map(([key, { user, windows }]) => {
        const name = presentName(user)
        return (
          <Tooltip key={key} title={windows > 1 ? `${name} (${windows} windows)` : name}>
            {user.actor ? (
              <Badge
                overlap="circular"
                anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                badgeContent={initials(user.name)}
                data-actor={user.actor}
                sx={{ '& .MuiBadge-badge': { fontSize: 7, height: 12, minWidth: 12, px: 0.25, bgcolor: user.color, color: '#fff' } }}
              >
                <Avatar alt={name} sx={{ bgcolor: 'background.paper', color: user.color, border: `1px solid ${user.color}` }}>
                  <SmartToyOutlined sx={{ fontSize: 15 }} />
                </Avatar>
              </Badge>
            ) : (
              <Avatar alt={name} sx={{ bgcolor: user.color }}>
                {initials(user.name)}
              </Avatar>
            )}
          </Tooltip>
        )
      })}
    </AvatarGroup>
  )
}

export default PresenceAvatars
