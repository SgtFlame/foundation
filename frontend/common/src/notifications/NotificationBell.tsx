// The notification centre (forge 26e-2): a bell in the side bar with the
// caller's unread count (notifications.count), a panel of their latest
// notifications (notifications.mine), a toast for each new one while the
// tab is shown and, once allowed, a browser notification while it is
// hidden. Opening one follows its workbench: link and marks it read.

import { useEffect, useRef, useState } from 'react'
import {
  Badge, Box, Button, IconButton, List, ListItemButton, ListItemText, Popover, Snackbar, Stack, Tooltip, Typography,
} from '@mui/material'
import NotificationsIcon from '@mui/icons-material/Notifications'
import NotificationsNoneIcon from '@mui/icons-material/NotificationsNone'
import { live } from '../live/LiveConnection.ts'
import { useQuery } from '../live/useQuery.ts'
import { log } from '../logging/Logger.ts'
import { openWorkbenchLink } from '../workbench/links.ts'
import type { EditorRegistry } from '../pages/workbench/EditorRegistry.ts'
import { ago, countOf, fresh, type Notification } from './notificationText.ts'

const bellLog = log.In('notifications').Tags('bell')

function browserAllows(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window && window.Notification.permission === 'granted'
}

function NotificationBell({ registry }: { registry: EditorRegistry }) {
  const count = countOf(useQuery<unknown>('notifications.count', {}).data)
  const mine = useQuery<{ records?: Notification[] }>('notifications.mine', { limit: 30 })
  const list = mine.data?.records ?? []
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const [toast, setToast] = useState<Notification>()
  const [allowed, setAllowed] = useState(browserAllows)
  const seen = useRef<Set<string>>(undefined)

  // New ones: a toast while the tab shows, a browser notification while hidden
  useEffect(() => {
    if (mine.loading) return
    const next = fresh(list, seen.current)
    seen.current = next.seen
    for (const n of next.fresh) {
      if (typeof document !== 'undefined' && document.hidden && browserAllows()) {
        try {
          const shown = new window.Notification(n.title ?? 'Notification', { body: n.body ?? '', tag: n._id })
          shown.onclick = () => {
            window.focus()
            void open(n)
          }
        } catch (err) {
          bellLog.fromError(err).warn('cannot show a browser notification')
        }
      } else {
        setToast(n)
      }
    }
    // open is stable enough: it reads the registry, which does not change
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mine.data, mine.loading])

  const markRead = (n: Notification) => {
    if (n.read) return
    live.command('record.update', n._id, { entity: 'notification', set: { read: true } }).catch((err) => bellLog.fromError(err).warn('cannot mark a notification read'))
  }

  const open = async (n: Notification) => {
    markRead(n)
    setAnchor(null)
    setToast(undefined)
    if (n.link && !(await openWorkbenchLink(registry, n.link))) bellLog.With('link', n.link).info('nothing opens %s', n.link)
  }

  const readAll = () => {
    live.call('notifications.readAll', {}, () => {}).catch((err) => bellLog.fromError(err).warn('cannot mark notifications read'))
  }

  const allow = async () => {
    if (!('Notification' in window)) return
    setAllowed((await window.Notification.requestPermission()) === 'granted')
  }

  return (
    <>
      <Tooltip title={count > 0 ? `${count} unread notification${count === 1 ? '' : 's'}` : 'Notifications'} placement="right">
        <IconButton aria-label="Notifications" onClick={(e) => setAnchor(e.currentTarget)} sx={{ borderRadius: 0, color: count > 0 ? 'text.primary' : 'text.secondary' }}>
          <Badge badgeContent={count} color="primary" max={99}>
            {count > 0 ? <NotificationsIcon /> : <NotificationsNoneIcon />}
          </Badge>
        </IconButton>
      </Tooltip>
      <Popover open={!!anchor} anchorEl={anchor} onClose={() => setAnchor(null)} anchorOrigin={{ vertical: 'center', horizontal: 'right' }} transformOrigin={{ vertical: 'center', horizontal: 'left' }}>
        <Box sx={{ width: 360, maxHeight: 520, display: 'flex', flexDirection: 'column' }} role="region" aria-label="Notifications">
          <Stack direction="row" sx={{ alignItems: 'center', px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
            <Typography variant="subtitle1" sx={{ flex: 1 }}>Notifications</Typography>
            <Button size="small" onClick={readAll} disabled={count === 0}>Mark all read</Button>
          </Stack>
          <Box sx={{ overflow: 'auto', flex: 1 }}>
            {list.length === 0 && (
              <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>{mine.loading ? 'Loading…' : 'Nothing yet. Mentions and reviews waiting for you show here.'}</Typography>
            )}
            <List dense disablePadding>
              {list.map((n) => (
                <ListItemButton key={n._id} onClick={() => void open(n)} sx={{ alignItems: 'flex-start', gap: 1 }}>
                  <Box sx={{ width: 8, height: 8, mt: 1, borderRadius: '50%', flexShrink: 0, bgcolor: n.read ? 'transparent' : 'primary.main' }} aria-label={n.read ? undefined : 'Unread'} />
                  <ListItemText
                    primary={n.title}
                    secondary={n.body}
                    slotProps={{ primary: { sx: { fontWeight: n.read ? 400 : 600 } }, secondary: { sx: { overflow: 'hidden', textOverflow: 'ellipsis', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' } } }}
                  />
                  <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0, mt: 0.5 }}>{ago(n.created)}</Typography>
                </ListItemButton>
              ))}
            </List>
          </Box>
          {!allowed && typeof window !== 'undefined' && 'Notification' in window && window.Notification.permission !== 'denied' && (
            <Box sx={{ px: 2, py: 1, borderTop: 1, borderColor: 'divider' }}>
              <Button size="small" onClick={() => void allow()}>Notify me while this tab is hidden</Button>
            </Box>
          )}
        </Box>
      </Popover>
      <Snackbar
        open={!!toast}
        autoHideDuration={6000}
        onClose={(_, reason) => reason !== 'clickaway' && setToast(undefined)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        message={toast ? [toast.title, toast.body].filter(Boolean).join(' · ') : ''}
        action={toast?.link ? <Button size="small" onClick={() => toast && void open(toast)}>Open</Button> : undefined}
      />
    </>
  )
}

export default NotificationBell
