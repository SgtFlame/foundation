// layout.flex: an application's own docked sub-tabs (flexlayout-react, as
// the workbench). The server sends the layout; each tab's content is the
// child keyed by the tab's id, rendered while shown. Selecting and closing
// tabs are events. The user's own arrangement is kept per user and page in
// the ui.layouts setting and used while its tabs are the server's.

import { Actions, Layout, Model, TabNode, type IJsonModel, type IJsonRowNode } from 'flexlayout-react'
import { useEffect, useMemo, useRef } from 'react'
import { Config } from '../../../services/Config.ts'
import { useSetting } from '../../../settings/configStore.ts'
import { log } from '../../../logging/index.ts'
import type { RendererProps } from '../../points.ts'

const logger = log.In('ui').Tags('layout')

interface LayoutProp {
  row: IJsonRowNode
}

interface SavedLayout {
  id: string
  model: string
}

function tabIds(row: unknown): string[] {
  const ids: string[] = []
  const walk = (n: { type?: string; id?: string; children?: unknown[] }) => {
    if (n.type === 'tab' && n.id) ids.push(n.id)
    for (const c of n.children ?? []) walk(c as never)
  }
  walk(row as never)
  return ids.sort()
}

function withComponents(row: IJsonRowNode): IJsonRowNode {
  const walk = (n: Record<string, unknown>): Record<string, unknown> => ({
    ...n,
    ...(n.type === 'tab' ? { component: 'ui', enableClose: n.closable === true } : {}),
    children: Array.isArray(n.children) ? n.children.map((c) => walk(c as Record<string, unknown>)) : n.children,
  })
  return walk(row as unknown as Record<string, unknown>) as unknown as IJsonRowNode
}

function selectedTabs(row: unknown): string[] {
  const out: string[] = []
  const walk = (n: { type?: string; selected?: number; children?: { id?: string }[] }) => {
    if (n.type === 'tabset') {
      const t = n.children?.[n.selected ?? 0]
      if (t?.id) out.push(t.id)
    }
    for (const c of n.children ?? []) walk(c as never)
  }
  walk(row as never)
  return out
}

async function save(id: string, model: Model) {
  try {
    const view = await Config.get({ section: 'ui.layouts', scope: 'user' })
    const layouts = ((view.value?.layouts as SavedLayout[]) ?? []).filter((l) => l.id !== id)
    layouts.push({ id, model: JSON.stringify(model.toJson().layout) })
    await Config.set({ section: 'ui.layouts', scope: 'user', patch: { layouts }, revision: view.revision })
  } catch (err) {
    logger.fromError(err).warn('cannot keep the arrangement of %s', id)
  }
}

function FlexLayout(p: RendererProps) {
  const layout = p.props.layout as LayoutProp | undefined
  const id = `${p.session.page}/${p.path.join('/') || p.node.key}`
  const saved = useSetting<SavedLayout[]>('ui.layouts', 'layouts')?.find((l) => l.id === id)
  const serverIds = JSON.stringify(tabIds(layout?.row ?? {}))
  const savedRow = useMemo(() => {
    if (!saved) return undefined
    try {
      const row = JSON.parse(saved.model) as IJsonRowNode
      return JSON.stringify(tabIds(row)) === serverIds ? row : undefined
    } catch {
      return undefined
    }
  }, [saved, serverIds])
  // The model is made again only when the set of tabs changes
  const model = useMemo(() => {
    const json: IJsonModel = {
      global: { tabSetEnableMaximize: false, tabEnableClose: false, tabSetEnableDeleteWhenEmpty: false },
      borders: [],
      layout: withComponents(savedRow ?? layout?.row ?? { type: 'row', children: [] }),
    }
    return Model.fromJson(json)
    // Only a change in the tabs, or the saved arrangement first loading, makes a new model
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverIds, !!savedRow])
  // The server selecting a tab
  const selected = JSON.stringify(selectedTabs(layout?.row ?? {}))
  useEffect(() => {
    for (const tab of JSON.parse(selected) as string[]) {
      const node = model.getNodeById(tab)
      if (node instanceof TabNode && !node.isSelected()) model.doAction(Actions.selectTab(tab))
    }
  }, [model, selected])
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)

  return (
    <div style={{ position: 'relative', width: '100%', height: '100%', minHeight: 240 }}>
      <Layout
        model={model}
        factory={(node) => <div style={{ height: '100%', overflow: 'auto' }}>{p.renderChild(node.getId())}</div>}
        onAction={(action) => {
          if (action.type === Actions.SELECT_TAB) p.emit('select', { id: action.data.tabNode })
          else if (action.type === Actions.DELETE_TAB) p.emit('close', { id: action.data.node })
          else if (action.type === Actions.MOVE_NODE) p.emit('move', { id: action.data.fromNode, to: action.data.toNode })
          return action
        }}
        onModelChange={(m, action) => {
          if (action.type !== Actions.MOVE_NODE && action.type !== Actions.ADJUST_WEIGHTS && action.type !== Actions.ADJUST_BORDER_SPLIT) return
          clearTimeout(timer.current)
          timer.current = setTimeout(() => void save(id, m), 800)
        }}
      />
    </div>
  )
}

export default FlexLayout
