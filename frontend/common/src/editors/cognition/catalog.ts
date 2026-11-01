import { useEffect, useState } from 'react'
import { log } from '../../logging/index.ts'
import { Cognition, type Conversion, type DataType, type Kind, type NodeType } from '../../services/Cognition.ts'

const cognitionLog = log.In('cognition')

// The node and data types, kinds and conversions, loaded once
export interface Catalog {
  nodeTypes: Map<string, NodeType>
  types: Map<string, DataType>
  kinds: Map<string, Kind>
  conversions: Conversion[]
}
let typesLoad: Promise<Catalog> | undefined
export function loadCatalog(): Promise<Catalog> {
  typesLoad ??= Cognition.nodeTypes().then((r) => ({
    nodeTypes: new Map(r.nodeTypes.map((t) => [t.id, t])),
    types: new Map(r.types.map((t) => [t.id, t])),
    kinds: new Map((r.kinds ?? []).map((k) => [k.id, k])),
    conversions: r.conversions ?? [],
  }))
  typesLoad.catch(() => (typesLoad = undefined))
  return typesLoad
}

export function useCatalog(): { catalog?: Catalog; error?: string } {
  const [catalog, setCatalog] = useState<Catalog>()
  const [error, setError] = useState<string>()
  useEffect(() => {
    loadCatalog()
      .then(setCatalog)
      .catch((err: unknown) => {
        cognitionLog.fromError(err).warn('cannot load the Cognition node types')
        setError(message(err))
      })
  }, [])
  return { catalog, error }
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
