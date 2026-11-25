// Asks the side bar to show a side view (forge 16c: RAG Search's link opens
// the Context view). The side bar listens; with none mounted it is a no-op.
const listeners = new Set<(id: string) => void>()

export function openSideView(id: string) {
  listeners.forEach((l) => l(id))
}

export function onOpenSideView(listener: (id: string) => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
