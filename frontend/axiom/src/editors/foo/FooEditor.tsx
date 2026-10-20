import type { FooObject } from './FooObject.ts'

function FooEditor({ object }: { object: FooObject }) {
  return <div>Foo editor: {object.id}</div>
}

export default FooEditor
