import { Actions, DockLocation, type Model } from 'flexlayout-react'
import type { BarObject } from '../../editors/bar/BarObject.ts'
import type { FooObject } from '../../editors/foo/FooObject.ts'
import type { VolumesObject } from '../../editors/volumes/VolumesObject.ts'

export type EditableObject = FooObject | BarObject | VolumesObject

const editorNames: Record<EditableObject['kind'], string> = {
  foo: 'Foo',
  bar: 'Bar',
  volumes: 'Volumes',
}

export class EditorRegistry {
  private readonly model: Model

  constructor(model: Model) {
    this.model = model
  }

  // Focuses the object's editor tab if one is open, otherwise opens a new one
  edit(object: EditableObject) {
    const tabId = `${object.kind}:${object.id}`
    if (this.model.getNodeById(tabId)) {
      this.model.doAction(Actions.selectTab(tabId))
      return
    }

    const tabset = this.model.getActiveTabset() ?? this.model.getFirstTabSet()
    if (!tabset) return

    this.model.doAction(
      Actions.addNode(
        {
          type: 'tab',
          id: tabId,
          name: `${editorNames[object.kind]} ${object.id}`,
          component: object.kind,
          config: { object },
        },
        tabset.getId(),
        DockLocation.CENTER,
        -1,
        true,
      ),
    )
  }
}
