import { useCallback, useEffect, useMemo, useState } from 'react'
import { Layout, Model, type IJsonModel, type TabNode } from 'flexlayout-react'
import { Alert, Box, CircularProgress, Stack } from '@mui/material'
import { log } from '../../logging/index.ts'
import {
  Cognition,
  type DataType,
  type GetGraphResponse,
  type GetTraceResponse,
  type NodeType,
  type Run,
} from '../../services/Cognition.ts'
import { useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import { CognitionContext, reachedBy, statsOf, type CognitionState, type Selection } from './cognitionState.ts'
import { CognitionObject } from './CognitionObject.ts'
import DetailsPanel from './DetailsPanel.tsx'
import GraphCanvas from './GraphCanvas.tsx'
import LogViewer from './LogViewer.tsx'

const cognitionLog = log.In('cognition')

const json: IJsonModel = {
  global: { tabEnableClose: false, tabSetEnableMaximize: true },
  borders: [],
  layout: {
    type: 'row',
    weight: 100,
    children: [
      {
        type: 'tabset',
        weight: 75,
        children: [
          { type: 'tab', id: 'logic', name: 'Logic Editor', component: 'logic' },
          { type: 'tab', id: 'log', name: 'Log Viewer', component: 'log' },
        ],
      },
      {
        type: 'tabset',
        weight: 25,
        children: [{ type: 'tab', id: 'details', name: 'Details', component: 'details' }],
      },
    ],
  },
}

function factory(node: TabNode) {
  switch (node.getComponent()) {
    case 'logic':
      return <GraphCanvas />
    case 'log':
      return <LogViewer />
    case 'details':
      return <DetailsPanel />
    default:
      return null
  }
}

// The node and data types, loaded once
let typesLoad: Promise<{ nodeTypes: Map<string, NodeType>; types: Map<string, DataType> }> | undefined
function loadTypes() {
  typesLoad ??= Cognition.nodeTypes().then((r) => ({
    nodeTypes: new Map(r.nodeTypes.map((t) => [t.id, t])),
    types: new Map(r.types.map((t) => [t.id, t])),
  }))
  typesLoad.catch(() => (typesLoad = undefined))
  return typesLoad
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// The graph's runs, newest first, paged
function useRuns(graphId: string) {
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
  useEffect(() => load(), [load])
  return useMemo(() => ({ runs, more, error, refresh: () => load(), loadMore: () => load(runs.at(-1)?.id) }), [runs, more, error, load])
}

// The Cognition Editor: a graph on a read-only canvas, its runs in the Log
// Viewer, and the details of what is selected (Cognition.md › Part 2).
// Editing, the Prompt Editor and debugging come in later passes.
function CognitionEditor({ object, tabId }: EditorProps<CognitionObject>) {
  const model = useMemo(() => Model.fromJson(json), [])
  const [graph, setGraph] = useState<GetGraphResponse>()
  const [types, setTypes] = useState<Awaited<ReturnType<typeof loadTypes>>>()
  const [error, setError] = useState<string>()
  const [selection, setSelection] = useState<Selection>({})
  const [chosenRun, setRunId] = useState<string>()
  // A trace, or why it could not be loaded, for the run it belongs to
  const [loaded, setLoaded] = useState<{ runId: string; trace?: GetTraceResponse; error?: string }>()
  const runs = useRuns(object.graphId)
  // The newest run is shown until another is chosen
  const runId = chosenRun ?? runs.runs[0]?.id
  const trace = loaded?.runId === runId ? loaded?.trace : undefined
  const traceError = loaded?.runId === runId ? loaded?.error : undefined

  useWorkbenchSubject(tabId, { type: 'cognition-graph', id: object.graphId, label: object.name, summary: object.helpText })

  useEffect(() => {
    Promise.all([Cognition.getGraph(object.graphId), loadTypes()])
      .then(([g, t]) => {
        setGraph(g)
        setTypes(t)
      })
      .catch((err: unknown) => {
        cognitionLog.fromError(err).warn('cannot open graph %s', object.graphId)
        setError(message(err))
      })
  }, [object.graphId])

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
      const g = graph?.graph
      if (!s.nodeId || !g || s.seq !== undefined) return
      const reaches = (run: { event: string }) => reachedBy(g, graph.pins ?? {}, run.event).has(s.nodeId!)
      const shown = runs.runs.find((r) => r.id === runId)
      if (shown && reaches(shown)) return
      const other = runs.runs.find(reaches)
      if (other) setRunId(other.id)
    },
    [graph, runs.runs, runId],
  )
  const state = useMemo<CognitionState | undefined>(() => {
    if (!graph?.graph || !graph.info || !types) return undefined
    return {
      info: graph.info,
      graph: graph.graph,
      pins: graph.pins ?? {},
      problems: graph.problems ?? [],
      nodeTypes: types.nodeTypes,
      types: types.types,
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
  }, [graph, types, runs, runId, trace, traceError, selection, select])

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
    </CognitionContext.Provider>
  )
}

export default CognitionEditor
