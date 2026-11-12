// The Settings border tab (documentation/Configuration.md): a search
// box and scope selector, a category tree from every section's category, and
// the forms of the selected category's sections.

import { useMemo, useState } from 'react'
import {
  Box,
  InputAdornment,
  List,
  ListItemButton,
  ListItemText,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from '@mui/material'
import SearchIcon from '@mui/icons-material/Search'
import type { Scope } from '../../services/Config.ts'
import { tenantOf, useCan, userIdOf, type Can } from '../../session/permissions.ts'
import { useSession, type SessionState } from '../../session/session.ts'
import { configuration, type ConfigurationContribution } from '../../settings/points.ts'
import { leaves, type Schema } from '../../settings/schema.ts'
import { useContributions } from '../../workbench/extensions/index.ts'
import SectionPanel from './SectionPanel.tsx'

interface Category {
  name: string
  path: string // the category path joined with '/'
  depth: number
}

// Every category path and its ancestors, depth first in first-seen order
function categories(sections: readonly ConfigurationContribution[]): Category[] {
  const seen = new Map<string, Category>()
  for (const s of sections) {
    s.category.forEach((name, i) => {
      const path = s.category.slice(0, i + 1).join('/')
      if (!seen.has(path)) seen.set(path, { name, path, depth: i })
    })
  }
  const all = [...seen.values()]
  const ordered: Category[] = []
  const visit = (parent: string, depth: number) => {
    for (const c of all.filter((c) => c.depth === depth && (depth === 0 || c.path.startsWith(parent + '/')))) {
      ordered.push(c)
      visit(c.path, depth + 1)
    }
  }
  visit('', 0)
  return ordered
}

function matches(section: ConfigurationContribution, query: string): boolean {
  const q = query.toLowerCase()
  const texts = [section.id, section.title, section.description ?? '']
  for (const leaf of leaves(section.schema as Schema)) {
    texts.push(leaf.path, leaf.schema.title ?? '', leaf.schema.description ?? '')
  }
  return texts.some((t) => t.toLowerCase().includes(q))
}

const scopes: { value: Scope; label: string; help: string }[] = [
  { value: 'app', label: 'App', help: 'Everyone, in every tenant' },
  { value: 'tenant', label: 'Tenant', help: 'Everyone in this tenant' },
  { value: 'user', label: 'User', help: 'Only you' },
]

// The resource of a section at a scope, as configservice checks it
function configResource(session: SessionState, scope: Scope, section: string): string {
  const at = scope === 'app' ? 'app' : scope === 'tenant' ? tenantOf(session) : `user:${userIdOf(session) ?? '0'}`
  return `${at}/config:${section}`
}

// Whether a section's secrets may be written at a scope: the app's by an
// Application Administrator, a tenant's or your own as configservice checks
function secretsWritable(can: Can, session: SessionState, scope: Scope, section: string): boolean {
  return scope === 'app' ? can('secret.write', 'app') : can('config.writeSecrets', configResource(session, scope, section))
}

// Whether settings may be written at a scope
function writable(can: Can, session: SessionState, scope: Scope): boolean {
  return can('config.write', configResource(session, scope, '*'))
}

function SettingsTab() {
  const sections = useContributions(configuration)
  const tree = useMemo(() => categories(sections), [sections])
  const [selected, setSelected] = useState<string>()
  const [query, setQuery] = useState('')
  const session = useSession()
  const can = useCan()
  const [chosen, setScope] = useState<Scope>()
  // Start where you may write: the app scope for its administrators, else your own
  const scope: Scope = chosen ?? (writable(can, session, 'app') ? 'app' : 'user')

  const category = selected ?? tree[0]?.path
  const shown = query
    ? sections.filter((s) => matches(s, query))
    : sections.filter((s) => {
        const path = s.category.join('/')
        return category !== undefined && (path === category || path.startsWith(category + '/'))
      })

  return (
    <Stack sx={{ height: '100%' }}>
      <Stack direction="row" spacing={1} sx={{ p: 1, borderBottom: 1, borderColor: 'divider', alignItems: 'center' }}>
        <TextField
          size="small"
          fullWidth
          placeholder="Search settings"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment> } }}
        />
        <ToggleButtonGroup size="small" exclusive value={scope} onChange={(_e, v: Scope | null) => v && setScope(v)} aria-label="Scope">
          {scopes.map((o) => (
            <Tooltip key={o.value} title={`${o.help}${writable(can, session, o.value) ? '' : ' (read-only for you)'}`}>
              <ToggleButton value={o.value}>{o.label}</ToggleButton>
            </Tooltip>
          ))}
        </ToggleButtonGroup>
      </Stack>
      <Stack direction="row" sx={{ flex: 1, minHeight: 0 }}>
        <List dense sx={{ width: 150, flexShrink: 0, overflow: 'auto', borderRight: 1, borderColor: 'divider', py: 0 }}>
          {tree.map((c) => (
            <ListItemButton
              key={c.path}
              selected={!query && c.path === category}
              onClick={() => {
                setSelected(c.path)
                setQuery('')
              }}
              sx={{ pl: 1.5 + c.depth * 1.5 }}
            >
              <ListItemText primary={c.name} />
            </ListItemButton>
          ))}
        </List>
        <Box sx={{ flex: 1, overflow: 'auto', p: 1.5 }}>
          {sections.length === 0 && (
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>
              No settings sections yet. Is the server running?
            </Typography>
          )}
          {query && shown.length === 0 && (
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>
              No settings match “{query}”.
            </Typography>
          )}
          {shown.map((s) => (
            <SectionPanel
              key={`${s.id}|${scope}`}
              section={s}
              scope={scope}
              readOnly={!can('config.write', configResource(session, scope, s.id))}
              secretsReadOnly={!secretsWritable(can, session, scope, s.id)}
            />
          ))}
        </Box>
      </Stack>
    </Stack>
  )
}

export default SettingsTab
