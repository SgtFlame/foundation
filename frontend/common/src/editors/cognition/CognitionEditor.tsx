import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Layout, Model, type IJsonModel, type IJsonTabNode, type TabNode } from 'flexlayout-react'
import { Alert, Box, CircularProgress, Snackbar, Stack } from '@mui/material'
import { usePresence } from '../../live/index.ts'
import { log } from '../../logging/index.ts'
import { Cognition, type GetTraceResponse, type Run } from '../../services/Cognition.ts'
import { useCatalog } from './catalog.ts'
import { useCan } from '../../session/permissions.ts'
import { useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import { CognitionContext, reachedBy, statsOf, type CognitionState, type Selection } from './cognitionState.ts'
import { CognitionObject } from './CognitionObject.ts'
import DetailsPanel from './DetailsPanel.tsx'
import { useDraft } from './editing.ts'
import GraphCanvas from './GraphCanvas.tsx'
import LogViewer from './LogViewer.tsx'
import PalettePanel from './PalettePanel.tsx'
import VariablesPanel from './VariablesPanel.tsx'

const cognitionLog = log.In('cognition')

// A tab another editor adds to the Graph Editor's layout, e.g. Forge's
// Test panel; it reads the graph with useCognition()
export interface GraphEditorTab {
  id: string
  name: string
  where?: 'left' | 'center' | 'right' // right by default
  render: () => ReactNode
}

function layoutJson(extra: GraphEditorTab[]): IJsonModel {
  const tab = (t: GraphEditorTab): IJsonTabNode => ({ type: 'tab', id: t.id, name: t.name, component: t.id })
  const at = (where: GraphEditorTab['where']) => extra.filter((t) => (t.where ?? 'right') === where).map(tab)
  return {
    global: { tabEnableClose: false, tabSetEnableMaximize: true },
    borders: [],
    layout: {
      type: 'row',
      weight: 100,
      children: [
        {
          type: 'tabset',
          weight: 17,
          children: [
            { type: 'tab', id: 'palette', name: 'Palette', component: 'palette' },
            { type: 'tab', id: 'variables', name: 'Variables', component: 'variables' },
            ...at('left'),
          ],
        },
        {
          type: 'tabset',
          weight: 60,
          children: [
            { type: 'tab', id: 'logic', name: 'Logic Editor', component: 'logic' },
            { type: 'tab', id: 'log', name: 'Log Viewer', component: 'log' },
            ...at('center'),
          ],
        },
        {
          type: 'tabset',
          weight: 23,
          children: [{ type: 'tab', id: 'details', name: 'Details', component: 'details' }, ...at('right')],
        },
      ],
    },
  }
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// The graph's runs, newest first, paged
function useRuns(graphId: string, published: number | undefined) {
  const [runs, setRuns] = useState<Run[]>([])
  const [more, setMore] = useState(false)
  const [error, setError] = useState<string>()
  const load = useCallback(
    (before?: string) => {
      Cognition.listRuns({ graphId, before, limit: 50 })
        .then((r) => {
          setRuns((old) => (before ? [...old, ...r.runs] : r.runs))
          setMore(!!r.more)
          setError(undefined)
        })
        .catch((err: unknown) => {
          cognitionLog.fromError(err).warn('cannot list the runs of graph %s', graphId)
          setError(message(err))
        })
    },
    [graphId],
  )
  // A new version brings new runs
  useEffect(() => load(), [load, published])
  return useMemo(() => ({ runs, more, error, refresh: () => load(), loadMore: () => load(runs.at(-1)?.id) }), [runs, more, error, load])
}

export interface GraphEditorProps {
  graphId: string
  tabId: string
  name?: string
  helpText?: string
  extraTabs?: GraphEditorTab[]
  openGraph?: (graphId: string, name: string) => void
  // The workbench subject's type: Forge's Blueprint Editor reports 'graph'
  subjectType?: string
}

// The Graph Editor: a graph's draft on an editable canvas, edited by
// several people at once over bp.* commands, its runs in the Log Viewer,
// the Palette and Variables, and the details of what is selected
// (Cognition.md › Built in slice 14). Forge's Blueprint Editor is this
// editor with its own tabs.
export function GraphEditor({ graphId, tabId, name, helpText, extraTabs = [], openGraph, subjectType = 'cognition-graph' }: GraphEditorProps) {
  const extraKey = extraTabs.map((t) => t.id).join(',')
  // The layout is made once per set of extra tabs
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const model = useMemo(() => Model.fromJson(layoutJson(extraTabs)), [extraKey])
  const { catalog, error: catalogError } = useCatalog()
  const editing = useDraft(graphId, catalog?.nodeTypes)
  const view = editing.view
  const can = useCan()
  const [selection, setSelection] = useState<Selection>({})
  const [selectedNodes, setSelectedNodes] = useState<string[]>([])
  const [chosenRun, setRunId] = useState<string>()
  const [loaded, setLoaded] = useState<{ runId: string; trace?: GetTraceResponse; error?: string }>()
  const runs = useRuns(graphId, view?.published)
  const presence = usePresence(`graph.${graphId}`, { selected: selectedNodes })
  const runId = chosenRun ?? runs.runs[0]?.id
  const trace = loaded?.runId === runId ? loaded?.trace : undefined
  const traceError = loaded?.runId === runId ? loaded?.error : undefined

  // The selected node is what "this node" means to the chat (forge 06g)
  useWorkbenchSubject(
    tabId,
    { type: subjectType, id: graphId, label: view?.graph.name ?? name ?? graphId, summary: helpText },
    { selection: selectedNodes.length ? { node: selectedNodes[0], items: selectedNodes.length > 1 ? selectedNodes : undefined } : undefined },
  )

  useEffect(() => {
    if (!runId) return
    Cognition.getTrace(runId)
      .then((t) => setLoaded({ runId, trace: t }))
      .catch((err: unknown) => {
        cognitionLog.fromError(err).warn('cannot load the trace of run %s', runId)
        setLoaded({ runId, error: message(err) })
      })
  }, [runId])

  // Selecting a node the shown run never reaches shows the newest run that does
  const select = useCallback(
    (s: Selection) => {
      setSelection(s)
      const g = view?.graph
      if (!s.nodeId || !g || s.seq !== undefined) return
      const reaches = (run: { event: string }) => reachedBy(g, view.pins ?? {}, run.event).has(s.nodeId!)
      const shown = runs.runs.find((r) => r.id === runId)
      if (shown && reaches(shown)) return
      const other = runs.runs.find(reaches)
      if (other) setRunId(other.id)
    },
    [view, runs.runs, runId],
  )
  const state = useMemo<CognitionState | undefined>(() => {
    if (!view || !catalog) return undefined
    const g = view.graph
    return {
      graphId,
      info: { id: graphId, name: g.name, description: g.description, kind: g.kind, version: view.published, builtIn: view.builtIn },
      view,
      editing,
      readOnly: view.readOnly || !can('cognition.write'),
      kinds: catalog.kinds,
      kind: catalog.kinds.get(g.kind),
      conversions: catalog.conversions,
      selectedNodes,
      setSelectedNodes,
      others: presence.others,
      openGraph,
      graph: g,
      pins: view.pins ?? {},
      problems: view.problems ?? [],
      nodeTypes: catalog.nodeTypes,
      types: catalog.types,
      runs: runs.runs,
      moreRuns: runs.more,
      runsError: runs.error,
      loadMoreRuns: runs.loadMore,
      refreshRuns: runs.refresh,
      runId,
      setRunId,
      trace,
      traceError,
      stats: statsOf(trace),
      selection,
      select,
    }
  }, [graphId, view, editing, can, catalog, selectedNodes, presence.others, openGraph, runs, runId, trace, traceError, selection, select])

  const factory = useCallback(
    (node: TabNode) => {
      switch (node.getComponent()) {
        case 'logic':
          return <GraphCanvas />
        case 'log':
          return <LogViewer />
        case 'details':
          return <DetailsPanel />
        case 'palette':
          return <PalettePanel />
        case 'variables':
          return <VariablesPanel />
        default:
          return extraTabs.find((t) => t.id === node.getComponent())?.render() ?? null
      }
    },
    [extraTabs],
  )

  const error = catalogError ?? editing.error?.message
  if (error) {
    return (
      <Box sx={{ p: 2 }}>
        <Alert severity="error">{error}</Alert>
      </Box>
    )
  }
  if (!state) {
    return (
      <Stack sx={{ height: '100%', alignItems: 'center', justifyContent: 'center' }}>
        <CircularProgress size={28} />
      </Stack>
    )
  }
  return (
    <CognitionContext.Provider value={state}>
      <Box sx={{ position: 'relative', width: '100%', height: '100%' }}>
        <Layout model={model} factory={factory} />
      </Box>
      <Snackbar open={!!editing.rejection} autoHideDuration={6000} onClose={editing.clearRejection}>
        <Alert severity="warning" variant="filled" onClose={editing.clearRejection}>
          {editing.rejection}
        </Alert>
      </Snackbar>
    </CognitionContext.Provider>
  )
}

// The Cognition Editor: the Graph Editor on a CognitionObject
function CognitionEditor({ object, tabId, registry }: EditorProps<CognitionObject>) {
  const openGraph = useCallback((id: string, name: string) => registry.edit(new CognitionObject({ id, name })), [registry])
  return <GraphEditor graphId={object.graphId} tabId={tabId} name={object.name} helpText={object.helpText} openGraph={openGraph} />
}

export default CognitionEditor
