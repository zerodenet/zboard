import { authenticatedAPI as api, getAuthToken } from './client'
let cached: { login: string; proof: string; expires: number } | undefined
export function clearAccountConfirmation() { cached = undefined }
export function accountConfirmation() {
 if (!cached || cached.login !== getAuthToken() || cached.expires <= Date.now()) { cached = undefined; return '' }
 return cached.proof
}
export async function confirmAccountPassword(password: string) {
 const login = getAuthToken()
 const { data } = await api.post('/account/security/confirm-password', { password })
 if (login !== getAuthToken()) throw new Error('登录状态已变化，请重新确认。')
 cached = { login, proof: data.data.confirmation, expires: Number(data.data.expires_at) * 1000 }
 return cached.proof
}
export async function changeAccountPassword(current_password: string, password: string) {
 await api.post('/account/security/password', { current_password, password })
 clearAccountConfirmation()
}
