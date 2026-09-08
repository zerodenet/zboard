import { authenticatedAPI as api } from './client'

export interface IdentityProvider { id: string; name: string }
export interface ExternalIdentity { id: string; plugin_id: string; issuer: string; created_at: string }
export interface IdentityLoginResult {
  linked?: boolean
  user?: { id: number; email: string; is_admin: boolean; status: string }
  auth?: { token: string; expires_at: number }
}
const data = <T>(r: { data: { data: T } }) => r.data.data
export const fetchIdentityProviders = () => api.get('/auth/oidc/providers').then(data<IdentityProvider[]>)
export const startIdentityLogin = (id: string) => api.post(`/auth/oidc/${encodeURIComponent(id)}/start`, {}).then(data<{ authorization_url: string }>)
export const finishIdentityLogin = () => api.post('/auth/oidc/finish', {}).then(data<IdentityLoginResult>)
export const fetchExternalIdentities = () => api.get('/account/identities').then(data<ExternalIdentity[]>)
export const bindExternalIdentity = (id: string, password: string) => api.post(`/account/identities/${encodeURIComponent(id)}/bind`, { password }).then(data<{ authorization_url: string }>)
export const unlinkExternalIdentity = (id: string, password: string) => api.post(`/account/identities/${encodeURIComponent(id)}/unlink`, { password }).then(data<{ unlinked: boolean }>)

export function navigateToIdentityProvider(raw: string) {
  const url = new URL(raw)
  if (url.protocol !== 'https:' || url.username || url.password) throw new Error('无效的身份提供方地址')
  window.location.assign(url.href)
}
