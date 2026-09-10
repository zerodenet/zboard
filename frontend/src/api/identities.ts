import { authenticatedAPI as api } from './client'

export interface IdentityLoginResult {
  registration_required?: boolean
  email?: string
  linked?: boolean
  user?: { id: number; email: string; is_admin: boolean; status: string }
  auth?: { token: string; expires_at: number }
}
const data = <T>(r: { data: { data: T } }) => r.data.data
export const finishIdentityLogin = (input: { email?: string; verification_code?: string } = {}) => api.post('/auth/oidc/finish', input).then(data<IdentityLoginResult>)
export const requestIdentityRegistrationCode = (email: string) => api.post('/auth/oidc/registration-code', { email }).then(data<{sent: boolean; resend_after: number}>)
export const fetchIdentityPasswordStatus = () => api.get('/account/identities/security').then(data<{password_set: boolean}>)
export const setupIdentityPassword = (password: string) => api.post('/auth/oidc/password', { password }).then(data<{password_set: boolean}>)
