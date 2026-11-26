// Administration › Moderation: Chronicle entries waiting for approval to
// be shared with every tenant
export class ModerationObject {
  readonly kind = 'moderation'
  readonly id = 'queue'
  readonly name = 'Moderation'
  readonly helpText = 'Approve or reject Chronicle entries asked to be shared with every tenant'
}
