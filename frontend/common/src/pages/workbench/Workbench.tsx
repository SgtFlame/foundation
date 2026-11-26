import { Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from '@mui/material'
import { Actions, Layout, Model, TabNode, TabSetNode, type IJsonModel, type IJsonRowNode } from 'flexlayout-react'
import 'flexlayout-react/style/dark.css'
import { useEffect, useRef, useState } from 'react'
import { logsOn } from '../../editors/logs/LogsObject.ts'
import { onOpenLogs } from '../../editors/logs/openLogs.ts'
import { fromSearch } from '../../editors/logs/view.ts'
import { live } from '../../live/index.ts'
import { log } from '../../logging/Logger.ts'
import WorkbenchContextProvider from '../../workbench/context/WorkbenchContextProvider.tsx'
import { workbenchContext } from '../../workbench/context/WorkbenchContextStore.ts'
import { startActivityFocus } from '../../workbench/context/activityFocus.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'
import { session } from '../../session/session.ts'
import { onKindsRemoved, openDeepLink } from '../../ui/editorKinds.ts'
import { layoutFromTabs, layoutToSave, readLayout, rowToShow, sameLayout, tabsOf, type LayoutNode, type SavedLayout, type SavedTabs } from '../../workspace/layout.ts'
import { LayoutSaver } from '../../workspace/layoutSaver.ts'
import { useWorkspaceWindows, workspaceWindows } from '../../workspace/windows.ts'
import WorkspaceWindowsDialog from '../../workspace/WorkspaceWindowsDialog.tsx'
import { useWorkspace, workspaces } from '../../workspace/workspaceStore.ts'
import { bordersJson, syncBorders } from './borders.ts'
import { createFactory } from './ComponentFactory.tsx'
import { EditorRegistry } from './EditorRegistry.ts'
import SideBar from './SideBar.tsx'

const wbLog = log.In('workbench').Tags('layout')

// Built on first render, after register.ts has loaded every contribution.
// The model is replaced when the tab shows another workspace's layout
// (forge 16b); the registry and factory stay.
let workbench: { model: Model; registry: EditorRegistry; factory: ReturnType<typeof createFactory> } | undefined

// Restorable editors (app editor kinds, and editors marked restorable) are
// saved in the workspace's layout and reopen from their objects; the others
// close on a reload or a switch. App tabs are restorable even before their
// kinds have loaded.
function restorable(kind: string): boolean {
  return kind.startsWith('app:') || !!extensions.get(editors).find((c) => c.id === kind)?.restorable
}

// The tabs saved in this browser before workspaces (per tenant and user),
// moved into Default once (forge 16b)
function savedTabsKey(): string {
  const account = session.get().account
  return account ? `workbench.tabs.${account.tenantId}.${account.user.id}` : 'workbench.tabs.local'
}

function loadSavedTabs(): SavedTabs {
  try {
    const raw = localStorage.getItem(savedTabsKey())
    const saved = raw ? (JSON.parse(raw) as SavedTabs) : undefined
    return Array.isArray(saved?.tabs) ? saved : { tabs: [] }
  } catch {
    return { tabs: [] }
  }
}

function forgetSavedTabs() {
  try {
    localStorage.removeItem(savedTabsKey())
  } catch {
    // storage unavailable: nothing was kept there
  }
}

const mainOf = (model: Model) => model.toJson().layout as unknown as LayoutNode
const layoutOf = (model: Model): SavedLayout => layoutToSave(mainOf(model), restorable)
// The main area's editor tabs, those keep accepts
const editorTabs = (model: Model, keep: (t: LayoutNode) => boolean = () => true) =>
  tabsOf(mainOf(model)).filter((t) => t.component !== 'borderTab' && keep(t))
const notRestorable = (t: LayoutNode) => !restorable(t.component ?? '')

// A model for row, keeping the borders as they are and the view state of
// every tab both show (no remount)
function modelFor(row: LayoutNode, previous?: Model): Model {
  const json: IJsonModel = {
    global: { tabSetEnableDeleteWhenEmpty: true },
    borders: previous ? previous.toJson().borders : bordersJson(),
    layout: row as unknown as IJsonRowNode,
  }
  return Model.fromJson(json, previous)
}

function getWorkbench() {
  if (!workbench) {
    // empty until the tab's workspace arrives; deep links open meanwhile
    const model = modelFor(rowToShow(undefined))
    const registry = new EditorRegistry(model)
    workbench = { model, registry, factory: createFactory(registry) }
  }
  return workbench
}

// Tells the workbench context store which editor tabs are open and which is
// focused, so focus tracking needs no editor code
function syncContext(model: Model) {
  const tabs: { tab: string; kind: string; title: string }[] = []
  model.visitNodes((node) => {
    if (node instanceof TabNode && node.getComponent() !== 'borderTab' && node.getParent() instanceof TabSetNode) {
      tabs.push({ tab: node.getId(), kind: node.getComponent() ?? '', title: node.getName() })
    }
  })
  workbenchContext.setLayout(tabs, model.getActiveTabset()?.getSelectedNode()?.getId())
}

// Saves one workspace's layout while this window may save it
function saverFor(id: string): LayoutSaver {
  return new LayoutSaver({
    layout: () => {
      const w = workspaceWindows.get()
      return w.workspace === id && w.mode === 'saving' && workbench ? layoutOf(workbench.model) : undefined
    },
    seen: () => {
      const w = workspaces.get().workspaces.find((x) => x.id === id)
      return w && { version: w.version, layout: w.layout }
    },
    send: async (version, layout) => {
      await live.command('workspace.update', id, { version, layout })
    },
    failed: (err) => wbLog.fromError(err).With('workspace', id).warn('cannot save the layout'),
  })
}

// Opens what the page's deep link names: the Log Explorer
// (?logs=<query>&range=1h), an app (?app=<appId>&route=/task/123) or a
// workbench link (?open=workbench:<type>/<id>)
function openLinks(registry: ReturnType<typeof getWorkbench>['registry']) {
  const linked = fromSearch(location.search)
  if (linked) registry.edit(logsOn(linked))
  openDeepLink(registry, location.search)
}

function Workbench() {
  const { registry, factory } = getWorkbench()
  const [model, setModel] = useState(() => getWorkbench().model)
  const { loaded, current } = useWorkspace()
  const windows = useWorkspaceWindows()
  // the workspace the model shows, the version it was laid out from (taken
  // or saved), and its saver
  const shown = useRef<string | undefined>(undefined)
  const base = useRef(0)
  const saver = useRef<LayoutSaver | undefined>(undefined)
  const lastMode = useRef(windows.mode)
  const [asking, setAsking] = useState<{ to: string; names: string[] }>()

  // Shows row in place of the current layout
  const show = (row: LayoutNode) => {
    const wb = getWorkbench()
    // the tab in front stays in front when the new layout has it (a deep
    // link's tab, or the tab this window was looking at)
    const front = wb.model.getActiveTabset()?.getSelectedNode()?.getId()
    const next = modelFor(row, wb.model)
    if (front && next.getNodeById(front)) {
      const tabset = next.getNodeById(front)!.getParent()
      if (tabset instanceof TabSetNode && tabset.getSelectedNode()?.getId() !== front) next.doAction(Actions.selectTab(front))
    }
    wb.model = next
    wb.registry.setModel(next)
    syncBorders(next)
    syncContext(next)
    setModel(next)
  }

  useEffect(() => extensions.subscribe(() => syncBorders(model)), [model])
  useEffect(() => {
    const listener = {
      onAfterAction: () => {
        syncContext(model)
        saver.current?.schedule()
      },
    }
    syncContext(model)
    model.addChangeListener(listener)
    return () => model.removeChangeListener(listener)
  }, [model])

  // The tab's workspace: its layout shown on load and on a switch; another
  // window's saved changes taken while this one follows (forge 16b)
  useEffect(() => {
    if (!loaded || !current) return
    if (shown.current !== current.id) {
      const first = shown.current === undefined
      saver.current?.retire() // saves the workspace left, as it was
      let layout = readLayout(current.layout)
      let moveIn: SavedLayout | undefined
      if (first && current.isDefault) {
        // the tabs this browser kept before workspaces become Default's
        // layout the first time; later browsers take the server's
        const legacy = loadSavedTabs()
        if (!layout && legacy.tabs.length) layout = moveIn = layoutFromTabs(legacy)
        else if (layout) forgetSavedTabs()
      }
      // on load, tabs a deep link opened stay; a switch closes the rest
      show(rowToShow(layout?.layout, first ? editorTabs(getWorkbench().model) : []))
      // a deep link opened before the layout came: its tab is focused again
      // in the layout shown (editing an open tab focuses it)
      if (first) openLinks(registry)
      shown.current = current.id
      base.current = current.version
      saver.current = saverFor(current.id)
      workspaceWindows.enter(current.id)
      if (moveIn) {
        live
          .command('workspace.update', current.id, { version: current.version, layout: moveIn })
          .then(forgetSavedTabs)
          .catch((err) => wbLog.fromError(err).warn('cannot move the saved tabs into Default'))
      } else if (first && !sameLayout(layout, layoutOf(getWorkbench().model))) {
        saver.current.schedule() // a deep link's tab joins the layout
      }
      return
    }
    const s = saver.current!
    s.versionSeen()
    if (current.version <= Math.max(base.current, s.acked)) return
    // a version this window did not make: another window's
    const theirs = readLayout(current.layout)
    if (windows.mode === 'unsaved') {
      workspaceWindows.noteChangedElsewhere()
      base.current = current.version
    } else if (windows.mode === 'saving' && s.idle()) {
      base.current = current.version
      if (!sameLayout(theirs, layoutOf(getWorkbench().model))) show(rowToShow(theirs?.layout, editorTabs(getWorkbench().model, notRestorable)))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, current?.id, current?.version, windows.mode])

  // This window's choice beside another window: following takes the saved
  // layout, keeping the editors that cannot be saved; a lone window saves
  // what it changed while it checked
  useEffect(() => {
    const was = lastMode.current
    lastMode.current = windows.mode
    if (windows.mode !== 'saving' || was === 'saving' || windows.workspace !== shown.current) return
    if (was === 'checking') {
      saver.current?.schedule()
      return
    }
    const now = workspaces.get().current
    if (!now || now.id !== shown.current) return
    saver.current?.reset()
    base.current = now.version
    show(rowToShow(readLayout(now.layout)?.layout, editorTabs(getWorkbench().model, notRestorable)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windows.mode, windows.workspace])

  // A switch closes the editors the other workspace does not have: one with
  // unsaved changes asks first
  useEffect(
    () =>
      workspaces.setSwitchGuard((to) => {
        const target = workspaces.get().workspaces.find((w) => w.id === to)
        const kept = new Set(tabsOf(readLayout(target?.layout)?.layout).map((t) => t.id))
        const dirty = workbenchContext.getState().editors.filter((e) => e.dirty && !kept.has(e.tab))
        if (dirty.length === 0) return true
        setAsking({ to, names: dirty.map((e) => e.title) })
        return false
      }),
    [],
  )

  // Saves a pending change when the page goes away
  useEffect(() => {
    const flush = () => saver.current?.flush()
    window.addEventListener('pagehide', flush)
    return () => window.removeEventListener('pagehide', flush)
  }, [])

  // Deep links (/workbench?logs=…, ?app=…, ?open=…) and the Log Explorer's requests from other views
  useEffect(() => {
    openLinks(registry)
    return onOpenLogs((view) => registry.edit(logsOn(view)))
  }, [registry])
  // The focused tab's subject, for the person's recent activity (forge 15c)
  useEffect(() => startActivityFocus(), [])
  // An app uninstalled, or an editor kind an upgrade removed: its tabs close
  useEffect(
    () =>
      onKindsRemoved((kinds) => {
        const gone: string[] = []
        getWorkbench().model.visitNodes((node) => {
          if (node instanceof TabNode && kinds.includes(node.getComponent() ?? '')) gone.push(node.getId())
        })
        for (const id of gone) registry.close(id)
      }),
    [registry],
  )

  const target = asking && workspaces.get().workspaces.find((w) => w.id === asking.to)
  return (
    <WorkbenchContextProvider>
      <main style={{ display: 'flex', flexGrow: 1, minHeight: 0 }}>
        <SideBar registry={registry} />
        <div style={{ position: 'relative', flexGrow: 1 }}>
          <Layout model={model} factory={factory} />
        </div>
      </main>
      <WorkspaceWindowsDialog />
      {asking && (
        <Dialog open onClose={() => setAsking(undefined)} maxWidth="xs">
          <DialogTitle>Switch to {target?.name ?? 'the other workspace'}?</DialogTitle>
          <DialogContent>
            <DialogContentText>
              {asking.names.join(', ')} {asking.names.length === 1 ? 'has' : 'have'} changes not saved yet. Switching closes {asking.names.length === 1 ? 'it' : 'them'}.
            </DialogContentText>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setAsking(undefined)} autoFocus>Stay</Button>
            <Button color="warning" onClick={() => { const to = asking.to; setAsking(undefined); workspaces.switchTo(to, true, true) }}>
              Switch anyway
            </Button>
          </DialogActions>
        </Dialog>
      )}
    </WorkbenchContextProvider>
  )
}

export default Workbench
