// Sending commands with optimistic local state: each pending command is
// kept until the subscriptions showing its target have seen its sequence,
// so the user's own change never flickers back (documentation/Live.md ›
// Reading your own writes).

import { useCallback, useMemo, useState } from 'react'
import { live, type Ack, type CommandOptions, type Position } from './LiveConnection.ts'

export interface PendingCommand {
  key: number
  command: string
  target: string
  args?: unknown
  ack?: Ack // set once committed
}

export interface CommandState {
  // Sends a command; resolves with its ack, rejects with CommandRejected
  send(command: string, target: string, args?: unknown, options?: CommandOptions): Promise<Ack>
  // The commands not yet reflected at pos: apply them over the query's data
  pending(pos: Position): PendingCommand[]
}

let next = 0

export function useCommand(): CommandState {
  const [list, setList] = useState<PendingCommand[]>([])
  const send = useCallback(async (command: string, target: string, args?: unknown, options?: CommandOptions) => {
    const key = ++next
    setList((l) => [...l, { key, command, target, args }])
    try {
      const ack = await live.command(command, target, args, options)
      setList((l) => l.map((p) => (p.key === key ? { ...p, ack } : p)))
      return ack
    } catch (err) {
      setList((l) => l.filter((p) => p.key !== key))
      throw err
    }
  }, [])
  const pending = useCallback(
    (pos: Position) => list.filter((p) => !p.ack || (pos[p.ack.stream] ?? 0) < p.ack.seq),
    [list],
  )
  return useMemo(() => ({ send, pending }), [send, pending])
}
