// The renderers of the starter library's primitives (forge 04e): the
// record.* family, presence.avatars and mui.breadcrumbs

import { useEffect, useSyncExternalStore, type ComponentType } from 'react'
import { Breadcrumbs, Link, Typography } from '@mui/material'
import { live } from '../../../live/LiveConnection.ts'
import { PresenceAvatars } from '../../../live/PresenceAvatars.tsx'
import type { PresentUser } from '../../../live/usePresence.ts'
import type { RendererProps } from '../../points.ts'
import { RecordFieldEditor } from './FieldEditor.tsx'
import { RecordFieldValue } from './FieldValue.tsx'
import { RecordForm } from './RecordForm.tsx'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

// presence.avatars: who is in an object, from its users prop (a presence:
// binding); with join, this window is in that object while it is shown
function PresenceAvatarsRenderer(p: RendererProps) {
  const join = str(p.props.join)
  useEffect(() => {
    if (!join) return
    live.setPresence(join, {})
    return () => live.setPresence(join, undefined)
  }, [join])
  const me = useSyncExternalStore((l) => live.onStatus(l), () => live.getHello()?.connection)
  const users = ((p.props.users as PresentUser[] | undefined) ?? []).filter((u) => p.props.self || u.connection !== me)
  return <PresenceAvatars users={users} max={(p.props.max as number) ?? 5} />
}

interface Crumb {
  label: string
  id?: string
}

function MuiBreadcrumbs(p: RendererProps) {
  const items = (p.props.items as Crumb[] | undefined) ?? []
  return (
    <Breadcrumbs aria-label="breadcrumbs">
      {items.map((c, i) =>
        i === items.length - 1 ? (
          <Typography key={i} variant="body2" color="text.primary">{c.label}</Typography>
        ) : (
          <Link key={i} component="button" variant="body2" underline="hover" color="inherit" onClick={() => p.emit('navigate', { id: c.id ?? c.label, index: i })}>
            {c.label}
          </Link>
        ),
      )}
    </Breadcrumbs>
  )
}

export const starterRenderers: Record<string, ComponentType<RendererProps>> = {
  'record.fieldEditor': RecordFieldEditor,
  'record.fieldValue': RecordFieldValue,
  'record.form': RecordForm,
  'presence.avatars': PresenceAvatarsRenderer,
  'mui.breadcrumbs': MuiBreadcrumbs,
}
