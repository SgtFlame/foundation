// The Volumes the person may read, kept current (forge 15a's kb.volumes),
// which a workspace's Volumes are checked among (forge 16c)
import { useQuery } from '../live/index.ts'

export interface VolumeItem {
  id: string
  name: string
  path: string // from the knowledge base root, with '/'
}

export function useVolumes(): { volumes: VolumeItem[]; loading: boolean } {
  const { data, loading } = useQuery<{ volumes?: VolumeItem[] }>('kb.volumes')
  return { volumes: data?.volumes ?? [], loading }
}
