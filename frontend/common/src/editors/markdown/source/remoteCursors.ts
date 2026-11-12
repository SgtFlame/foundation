import { useEffect } from 'react'
import type { Awareness } from 'y-protocols/awareness'
import type { LocalUser } from '../collab/localUser.ts'

// y-monaco marks each remote client's selection and caret with
// yRemoteSelection-<id> and yRemoteSelectionHead-<id>; this colours them and
// labels the caret with the user's name.
function cursorStyles(awareness: Awareness): string {
  const rules: string[] = []
  awareness.getStates().forEach((state, clientId) => {
    const user = state.user as LocalUser | undefined
    if (clientId === awareness.clientID || !user) return
    const name = JSON.stringify(user.name)
    rules.push(
      `.yRemoteSelection-${clientId} { background-color: ${user.color}40; }`,
      `.yRemoteSelectionHead-${clientId} { position: absolute; height: 100%; border-left: 2px solid ${user.color}; }`,
      `.yRemoteSelectionHead-${clientId}::after { content: ${name}; position: absolute; top: -1.2em; left: -2px; padding: 0 4px; font-size: 11px; line-height: 1.2em; white-space: nowrap; color: #fff; background: ${user.color}; border-radius: 3px 3px 3px 0; pointer-events: none; }`,
    )
  })
  return rules.join('\n')
}

// Keeps a style element with the remote cursors' colours while mounted
export function useRemoteCursorStyles(awareness: Awareness) {
  useEffect(() => {
    const style = document.createElement('style')
    document.head.append(style)
    const update = () => {
      style.textContent = cursorStyles(awareness)
    }
    update()
    awareness.on('change', update)
    return () => {
      awareness.off('change', update)
      style.remove()
    }
  }, [awareness])
}
