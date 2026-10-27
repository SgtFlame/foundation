// The chat levels and the model catalog, as the browser reads them from
// Settings (ai.chat.levels and ai.models), with cost formatting.

import { useSetting } from '../../settings/configStore.ts'

export interface ChatLevel {
  id: string
  label: string
  icon?: string
  modelRef: string // an ai.models id
  default?: boolean
}

export interface CatalogModel {
  id: string
  title?: string
  model: string // the provider's model id
  contextWindow?: number
  pricing?: { currency?: string; inputPerMTok?: number; outputPerMTok?: number }
}

export function useLevels(): { levels: readonly ChatLevel[]; models: readonly CatalogModel[] } {
  const levels = useSetting<ChatLevel[]>('ai.chat.levels', 'levels', 'user') ?? []
  const models = useSetting<CatalogModel[]>('ai.models', 'models') ?? []
  return { levels, models }
}

export function defaultLevel(levels: readonly ChatLevel[]): ChatLevel | undefined {
  return levels.find((l) => l.default) ?? levels[0]
}

// "$0.0012", with more digits for fractions of a cent
export function formatCost(micros: number, currency = 'USD'): string {
  const amount = micros / 1_000_000
  const symbol = currency === 'USD' ? '$' : `${currency} `
  return `${symbol}${amount > 0 && amount < 0.01 ? amount.toFixed(5) : amount.toFixed(4)}`
}

// "3.2k"
export function formatTokens(n: number): string {
  return n >= 10_000 ? `${Math.round(n / 1000)}k` : n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n)
}
