// What the tab hosting a UI session offers its renderers: everyone present
// in the tab's object, and this window's presence state there (merged), so
// a graph can show who has selected what.

import { createContext, useContext } from 'react'
import type { PresentUser } from '../live/usePresence.ts'

export interface UiHost {
  others: PresentUser[]
  setPresence(state: Record<string, unknown>): void
}

export const UiHostContext = createContext<UiHost>({ others: [], setPresence: () => {} })

export function useUiHost(): UiHost {
  return useContext(UiHostContext)
}
