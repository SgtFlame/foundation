import { useCallback, useEffect, useState } from 'react'
import { Volume, type ListFolderResponse } from '../../services/Volume.ts'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// Lists the folder at path (folders from the knowledge base root), reloading
// when the path changes or after each run
export function useFolderListing(path: readonly string[]) {
  const [listing, setListing] = useState<ListFolderResponse>()
  const [error, setError] = useState<string>()
  const [reload, setReload] = useState(0)
  const key = JSON.stringify(path)

  const refresh = useCallback(() => setReload((n) => n + 1), [])

  useEffect(() => {
    let current = true
    Volume.listFolder({ path: { folders: JSON.parse(key) as string[] } })
      .then((response) => current && setListing(response))
      .catch((err: unknown) => current && setError(errorMessage(err)))
    return () => {
      current = false
    }
  }, [key, reload])

  // Runs an operation, shows its error if it fails, then reloads the listing
  const run = useCallback(
    async (operation: () => Promise<unknown>) => {
      setError(undefined)
      try {
        await operation()
      } catch (err) {
        setError(errorMessage(err))
      }
      refresh()
    },
    [refresh],
  )

  return {
    folders: listing?.folders ?? [],
    files: listing?.files ?? [],
    error,
    clearError: () => setError(undefined),
    refresh,
    run,
  }
}
