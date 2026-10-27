import { Actions, DockLocation, type Model } from 'flexlayout-react'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors, type EditableObject } from '../../workbench/points.ts'

export type { EditableObject } from '../../workbench/points.ts'

// The tab an object is edited in
export function tabIdOf(object: EditableObject): string {
  return `${object.kind}:${object.id}`
}

export class EditorRegistry {
  private readonly model: Model

  constructor(model: Model) {
    this.model = model
  }

  // Focuses the object's editor tab if one is open, otherwise opens a new
  // one. The tab shows the workbench.editor contribution of the object's
  // kind, or a placeholder when there is none.
  edit(object: EditableObject) {
    const tabId = tabIdOf(object)
    if (this.model.getNodeById(tabId)) {
      this.model.doAction(Actions.selectTab(tabId))
      return
    }

    const tabset = this.model.getActiveTabset() ?? this.model.getFirstTabSet()
    if (!tabset) return

    const editorName = extensions.get(editors).find((c) => c.id === object.kind)?.name ?? object.kind
    this.model.doAction(
      Actions.addNode(
        {
          type: 'tab',
          id: tabId,
          name: object.name ?? `${editorName} ${object.id}`,
          helpText: object.helpText,
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

  // Focuses an open editor tab; false when there is no such tab
  focus(tabId: string): boolean {
    if (!this.model.getNodeById(tabId)) return false
    this.model.doAction(Actions.selectTab(tabId))
    return true
  }

  // Renames an open editor tab, e.g. after its object is renamed
  rename(tabId: string, name: string) {
    if (this.model.getNodeById(tabId)) this.model.doAction(Actions.renameTab(tabId, name))
  }

  // Closes an editor tab, e.g. after its object is deleted
  close(tabId: string) {
    if (this.model.getNodeById(tabId)) this.model.doAction(Actions.deleteTab(tabId))
  }
}
