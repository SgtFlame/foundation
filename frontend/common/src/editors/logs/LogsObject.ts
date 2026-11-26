import { toSearch, type LogsView } from './view.ts'

// The Log Explorer, opened from the Administration section, or on a query
// from a deep link or another view (each such query in its own tab)
export class LogsObject {
  readonly kind = 'logs'
  readonly id: string
  readonly name: string
  readonly view?: LogsView // the query and range it opens on

  constructor(id: string, view?: LogsView) {
    this.id = id
    this.view = view
    this.name = id === 'explorer' ? 'Log Explorer' : `Logs: ${view?.query ?? id}`.slice(0, 40)
  }
}

// A Log Explorer on a query, in a tab of its own; opening the same query
// and range again focuses that tab
export function logsOn(view: LogsView): LogsObject {
  let hash = 0
  for (const c of toSearch(view)) hash = (Math.imul(hash, 31) + c.charCodeAt(0)) | 0
  return new LogsObject(`query-${(hash >>> 0).toString(36)}`, view)
}
