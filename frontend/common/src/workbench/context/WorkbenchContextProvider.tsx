import type { ReactNode } from 'react'
import { StoreContext } from './WorkbenchContext.ts'
import { workbenchContext } from './WorkbenchContextStore.ts'

// Hands the browser tab's workbench context store to the editors
function WorkbenchContextProvider({ children }: { children: ReactNode }) {
  return <StoreContext.Provider value={workbenchContext}>{children}</StoreContext.Provider>
}

export default WorkbenchContextProvider
