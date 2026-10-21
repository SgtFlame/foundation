import { Actions, DockLocation, type Model } from 'flexlayout-react'
import type { BarObject } from '../../editors/bar/BarObject.ts'
import type { FooObject } from '../../editors/foo/FooObject.ts'
import type { MarkdownObject } from '../../editors/markdown/MarkdownObject.ts'
import type { VolumeObject } from '../../editors/volume/VolumeObject.ts'
import type { VolumesObject } from '../../editors/volumes/VolumesObject.ts'

// An object may set its own tab text (name) and tab hover text (helpText)
export type EditableObject = FooObject | BarObject | VolumesObject | VolumeObject | MarkdownObject

const editorNames: Record<EditableObject['kind'], string> = {
  foo: 'Foo',
  bar: 'Bar',
  volumes: 'Volumes',
  volume: 'Volume',
  markdown: 'Markdown',
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
          name: 'name' in object ? object.name : `${editorNames[object.kind]} ${object.id}`,
          helpText: 'helpText' in object ? object.helpText : undefined,
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
