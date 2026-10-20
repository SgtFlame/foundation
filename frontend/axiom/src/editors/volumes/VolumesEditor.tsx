import type { VolumesObject } from './VolumesObject.ts'

function VolumesEditor({ object }: { object: VolumesObject }) {
  return <div>Volumes editor: {object.id}</div>
}

export default VolumesEditor
