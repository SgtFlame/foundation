import { useState } from 'react'
import { useNavigate } from 'react-router'
import { Avatar, Divider, IconButton, ListItemIcon, ListItemText, ListSubheader, Menu, MenuItem, Tooltip } from '@mui/material'
import CheckIcon from '@mui/icons-material/Check'
import LogoutIcon from '@mui/icons-material/Logout'
import ManageAccountsIcon from '@mui/icons-material/ManageAccounts'
import PersonIcon from '@mui/icons-material/Person'
import { session, useSession } from './session.ts'
import { AccountObject } from '../editors/account/AccountObject.ts'
import type { EditorRegistry } from '../pages/workbench/EditorRegistry.ts'
import { log } from '../logging/Logger.ts'

const menuLog = log.In('identity').Tags('menu')

function initials(name: string): string {
  const parts = name.trim().split(/\s+/)
  return ((parts[0]?.[0] ?? '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase() || '?'
}

// The account button at the bottom of the side bar: who is signed in, the
// tenant switcher, the account page and signing out. In single-user mode it
// offers setting up accounts.
function AccountMenu({ registry }: { registry: EditorRegistry }) {
  const s = useSession()
  const navigate = useNavigate()
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const close = () => setAnchor(null)
  const account = s.account
  const tenant = account?.tenants.find((t) => t.id === account.tenantId)
  const title = account ? `${account.user.displayName} · ${tenant?.name ?? 'no tenant'}` : 'Single-user mode'

  const switchTo = async (tenantId: string) => {
    close()
    if (tenantId === account?.tenantId) return
    try {
      await session.switchTenant(tenantId)
    } catch (err) {
      menuLog.fromError(err).error('cannot switch tenants')
    }
  }

  return (
    <>
      <Tooltip title={title} placement="right">
        <IconButton aria-label={`Account: ${title}`} aria-haspopup="menu" onClick={(e) => setAnchor(e.currentTarget)} sx={{ borderRadius: 0 }}>
          {account ? <Avatar sx={{ width: 26, height: 26, fontSize: 12 }}>{initials(account.user.displayName)}</Avatar> : <PersonIcon />}
        </IconButton>
      </Tooltip>
      <Menu anchorEl={anchor} open={!!anchor} onClose={close} anchorOrigin={{ vertical: 'top', horizontal: 'right' }} transformOrigin={{ vertical: 'bottom', horizontal: 'left' }}>
        {account ? (
          [
            <ListSubheader key="who" sx={{ lineHeight: 2.5 }}>
              {account.user.displayName} ({account.user.username})
            </ListSubheader>,
            <ListSubheader key="tenants" sx={{ lineHeight: 2, fontSize: 11 }}>
              Tenants
            </ListSubheader>,
            ...account.tenants.map((t) => (
              <MenuItem key={t.id} onClick={() => void switchTo(t.id)} selected={t.id === account.tenantId}>
                <ListItemIcon>{t.id === account.tenantId && <CheckIcon fontSize="small" />}</ListItemIcon>
                <ListItemText>{t.name}</ListItemText>
              </MenuItem>
            )),
            <Divider key="d" />,
            <MenuItem key="account" onClick={() => { close(); registry.edit(new AccountObject()) }}>
              <ListItemIcon><ManageAccountsIcon fontSize="small" /></ListItemIcon>
              <ListItemText>Account…</ListItemText>
            </MenuItem>,
            <MenuItem key="out" onClick={() => { close(); void session.logout() }}>
              <ListItemIcon><LogoutIcon fontSize="small" /></ListItemIcon>
              <ListItemText>Sign out</ListItemText>
            </MenuItem>,
          ]
        ) : (
          [
            <ListSubheader key="who" sx={{ lineHeight: 2.5 }}>
              Single-user mode: no sign-in
            </ListSubheader>,
            <MenuItem key="setup" onClick={() => { close(); navigate('/setup?next=/workbench') }}>
              <ListItemIcon><ManageAccountsIcon fontSize="small" /></ListItemIcon>
              <ListItemText>Set up accounts…</ListItemText>
            </MenuItem>,
          ]
        )}
      </Menu>
    </>
  )
}

export default AccountMenu
