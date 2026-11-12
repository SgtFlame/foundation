export function formatSize(bytes: number | undefined): string {
  if (bytes === undefined) return '—'
  if (bytes < 1000) return `${bytes} B`
  return `${(bytes / 1000).toFixed(1)} kB`
}

export function formatDuration(ms: number | undefined): string {
  if (ms === undefined) return '—'
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(2)} s`
}

// The path and query of a request URL, as Chrome's Name column shows it
export function shortName(url: string): string {
  try {
    const parsed = new URL(url, window.location.origin)
    return `${parsed.pathname}${parsed.search}`
  } catch {
    return url
  }
}
