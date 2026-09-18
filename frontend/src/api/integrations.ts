import { authenticatedAPI as api } from './client'
export interface IntegrationCredential {
  id: number
  name: string
  token_prefix: string
  scopes: string[]
  expires_at: string | null
  revoked_at: string | null
  created_at: string
}
export async function listIntegrationCredentials(offset = 0, limit = 20): Promise<IntegrationCredential[]> {
  const { data } = await api.get('/account/integrations/credentials', { params: { offset, limit } })
  return data.data.items
}
export async function issueIntegrationCredential(name: string, days: number): Promise<{ credential: IntegrationCredential; token: string }> {
  const { data } = await api.post('/account/integrations/credentials', {
    name, scopes: ['metering.usage.query'], expires_at: new Date(Date.now() + days * 86400000).toISOString(),
  })
  return data.data
}
export async function revokeIntegrationCredential(id: number) {
  await api.delete(`/account/integrations/credentials/${id}`)
}
