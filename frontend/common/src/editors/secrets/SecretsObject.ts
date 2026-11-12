// Administration › Secrets: the server's secrets in Vault and the app
// scope's secret settings, write-only
export class SecretsObject {
  readonly kind = 'secrets'
  readonly id = 'server'
  readonly name = 'Secrets'
  readonly helpText = 'Set and delete the server’s secrets; their values are never shown'
}
