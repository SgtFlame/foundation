import type { BarObject } from './BarObject.ts'

function BarEditor({ object }: { object: BarObject }) {
  return <div>Bar editor: {object.id}</div>
}

export default BarEditor
