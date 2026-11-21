// Undo and redo by request (forge 06b): an editor's Undo reverses the
// caller's last request in its scope, from the operation log on the
// server, so it survives a reload, is shared by the user's windows, and
// leaves what others changed meanwhile.

import { useCallback, useMemo, useState, type KeyboardEvent } from 'react'
import { live } from './LiveConnection.ts'
import { useQuery } from './useQuery.ts'
import type { UndoArgs, UndoResult, UndoStack } from '../services/Apps.ts'
import { ServiceError } from '../services/http/client.ts'

// A refused undo with what undoing the rest would send: the conflicts,
// and the ops of a forced dry run
export async function withRest(call: 'ops.undo' | 'ops.redo', res: UndoResult): Promise<UndoResult> {
  if (res.status !== 'conflicts') return res
  const rest = await runUndo(call, { request: res.request, force: true, dryRun: true })
  return { ...rest, status: 'conflicts', conflicts: res.conflicts }
}

// Runs ops.undo or ops.redo; resolves with its result
export function runUndo(call: 'ops.undo' | 'ops.redo', args: UndoArgs): Promise<UndoResult> {
  return new Promise<UndoResult>((resolve, reject) => {
    let result: UndoResult | undefined
    live
      .call(call, args, (event, data) => {
        if (event === 'result') result = data as UndoResult
      })
      .then(() => (result ? resolve(result) : reject(new Error(`${call} ended without a result`))), reject)
  })
}

export interface UndoState {
  stack?: UndoStack
  canUndo: boolean
  canRedo: boolean
  undoTitle: string // what Undo would undo, for its tooltip
  redoTitle: string
  undo(): Promise<void>
  redo(): Promise<void>
  busy: boolean
  // An undo or redo refused for what others changed since: force undoes
  // the rest, leaving those items as the others left them
  conflict?: { result: UndoResult; redo: boolean }
  force(): Promise<void>
  dismiss(): void
  error?: string
  clearError(): void
  // The last undo or redo done, for an editor to follow (the selection)
  last?: UndoResult
}

// The caller's undo and redo in a scope (app:<id>, app:<id>/manifest,
// prefab:<id>, graph:<id>, entity:<id>). Undo first waits for the
// commands this window has in flight, so it reaches them.
export function useUndo(scope: string | undefined): UndoState {
  const q = useQuery<UndoStack>(scope ? 'ops.stack' : undefined, { scope })
  const [busy, setBusy] = useState(false)
  const [conflict, setConflict] = useState<{ result: UndoResult; redo: boolean }>()
  const [error, setError] = useState<string>()
  const [last, setLast] = useState<UndoResult>()

  const run = useCallback(
    async (redo: boolean, args: UndoArgs) => {
      if (!scope) return
      setBusy(true)
      setError(undefined)
      try {
        await live.settled()
        const call = redo ? 'ops.redo' : 'ops.undo'
        const res = await runUndo(call, args)
        if (res.status === 'conflicts') setConflict({ result: await withRest(call, res), redo })
        else {
          setConflict(undefined)
          if (res.status === 'done') setLast(res)
        }
      } catch (err) {
        setError(err instanceof ServiceError || err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(false)
      }
    },
    [scope],
  )
  const undo = useCallback(() => run(false, { scope }), [run, scope])
  const redo = useCallback(() => run(true, { scope }), [run, scope])
  const force = useCallback(async () => {
    if (conflict) await run(conflict.redo, { request: conflict.result.request, force: true })
  }, [conflict, run])

  const stack = q.data
  return useMemo(
    () => ({
      stack,
      canUndo: !busy && !!stack?.undo.length,
      canRedo: !busy && !!stack?.redo.length,
      undoTitle: stack?.undo[0] ? `Undo ${stack.undo[0].summary}` : 'Nothing to undo',
      redoTitle: stack?.redo[0] ? `Redo ${stack.redo[0].summary}` : 'Nothing to redo',
      undo,
      redo,
      busy,
      conflict,
      force,
      dismiss: () => setConflict(undefined),
      error,
      clearError: () => setError(undefined),
      last,
    }),
    [stack, busy, undo, redo, conflict, force, error, last],
  )
}

// Ctrl+Z undoes, Ctrl+Shift+Z and Ctrl+Y redo, while the editor has focus;
// in a text field the field's own undo is left alone. Put it on the
// editor's root as onKeyDown.
export function undoKeys(state: Pick<UndoState, 'undo' | 'redo'>) {
  return (e: KeyboardEvent) => {
    if (!(e.ctrlKey || e.metaKey) || e.altKey) return
    const t = e.target as HTMLElement
    if (t.isContentEditable || t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.closest('.monaco-editor, .cm-editor')) return
    const key = e.key.toLowerCase()
    if (key === 'z') {
      e.preventDefault()
      e.stopPropagation()
      void (e.shiftKey ? state.redo() : state.undo())
    } else if (key === 'y') {
      e.preventDefault()
      e.stopPropagation()
      void state.redo()
    }
  }
}
