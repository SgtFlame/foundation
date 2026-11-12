// Renders a server-described tree with the ui.renderer contributions
// (documentation/ServerUI.md). A node of an unknown type, or one whose
// renderer fails, shows an inline error box rather than nothing. In design
// mode (the Prefab Editor's Canvas) each node with a src is wrapped in an
// element carrying it as data-src, which takes no part in layout, so the
// Canvas can find a clicked node and outline it; in 'select' mode events
// are not sent.

import { Alert, Box } from '@mui/material'
import { Component, memo, useMemo, useSyncExternalStore, type ReactNode } from 'react'
import { useContributions } from '../workbench/extensions/index.ts'
import { renderers, type RendererContribution } from './points.ts'
import type { UiNode } from './types.ts'
import type { UiSession } from './UiSession.ts'

// How a design session's tree is drawn: 'select' wraps nodes and sends no
// events, 'interact' wraps them and sends events
export type DesignMode = 'select' | 'interact'

export function ErrorBox({ messages }: { messages: string[] }) {
  return (
    <Alert severity="error" variant="outlined" sx={{ my: 0.5 }}>
      {messages.map((m, i) => (
        <div key={i}>{m}</div>
      ))}
    </Alert>
  )
}

class NodeBoundary extends Component<{ type: string; children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {}
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error) return <ErrorBox messages={[`${this.props.type} failed to render: ${this.state.error.message}`]} />
    return this.props.children
  }
}

interface NodeViewProps {
  node: UiNode
  path: string[]
  session: UiSession
  byType: Map<string, RendererContribution>
  design?: DesignMode
}

const NodeView = memo(function NodeView({ node, path, session, byType, design }: NodeViewProps) {
  // Only the local paths this node binds re-render it, as a stable string
  const localBinds = Object.entries(node.bind ?? {}).filter(([, src]) => src.startsWith('local.'))
  const localKey = useSyncExternalStore(session.local.subscribe, () =>
    localBinds.length ? JSON.stringify(localBinds.map(([, src]) => session.local.get(src.slice(6)) ?? null)) : '',
  )
  const contribution = byType.get(node.type)
  const props = useMemo(() => {
    const out = { ...node.props }
    if (localKey) {
      const values = JSON.parse(localKey) as unknown[]
      for (const [i, [prop]] of localBinds.entries()) if (values[i] !== null) out[prop] = values[i]
    }
    return out
    // localBinds follows node
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node, localKey])

  const childPath = (key: string) => [...path, key]
  const render = (child: UiNode) => <NodeView key={child.key} node={child} path={childPath(child.key)} session={session} byType={byType} design={design} />
  const view = contribution ? rendered(contribution) : <ErrorBox messages={[`there is no renderer for ${node.type}`]} />
  if (!design || !node.src) return view
  return (
    <div data-src={node.src} style={{ display: 'contents' }}>
      {view}
    </div>
  )

  function rendered({ component: Renderer }: RendererContribution) {
    return (
      <NodeBoundary type={node.type}>
        <Renderer
          node={node}
          path={path}
          props={props}
          session={session}
          emit={(event, data) => {
            const on = node.on?.[event]
            if (on && design !== 'select') session.emit(path, on, data)
          }}
          handles={(event) => !!node.on?.[event]}
          setLocal={(prop, value) => {
            const src = node.bind?.[prop]
            if (!src?.startsWith('local.')) return false
            session.local.set(src.slice(6), value)
            return true
          }}
          renderChild={(key) => {
            const child = node.children?.find((c) => c.key === key)
            return child ? render(child) : null
          }}
          slot={(name) => (node.slots?.[name] ?? []).map((c) => <NodeView key={c.key} node={c} path={childPath(c.key)} session={session} byType={byType} design={design} />)}
        >
          {(node.children ?? []).map(render)}
        </Renderer>
      </NodeBoundary>
    )
  }
})

// Renders a session's tree; design draws a design session's (see above)
export function UiRenderer({ session, tree, design }: { session: UiSession; tree: UiNode; design?: DesignMode }) {
  const contributions = useContributions(renderers)
  const byType = useMemo(() => new Map(contributions.map((c) => [c.id, c])), [contributions])
  return (
    <Box sx={{ height: '100%' }}>
      <NodeView node={tree} path={[]} session={session} byType={byType} design={design} />
    </Box>
  )
}

export default UiRenderer
