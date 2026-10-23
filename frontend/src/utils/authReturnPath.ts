// A short-lived navigation hint for the existing full-page identity flow.
// Never store credentials or drafts here, and never restore another account's context.
const key = 'zboard.auth.return-path'
const maxAgeMs = 10 * 60_000

function allowed(path: string, isAdmin: boolean) {
  return path === '/account' || path.startsWith('/account/') || path.startsWith('/account?') || path.startsWith('/account#')
    || isAdmin && (path === '/admin' || path.startsWith('/admin/') || path.startsWith('/admin?') || path.startsWith('/admin#'))
}

export function clearAuthReturnPath() {
  try { sessionStorage.removeItem(key) } catch { /* Navigation remains usable without optional storage. */ }
}

export function rememberAuthReturnPath(path: string, accountID: number) {
  clearAuthReturnPath()
  if (!allowed(path, true) || !Number.isSafeInteger(accountID) || accountID < 1) return
  try { sessionStorage.setItem(key, JSON.stringify({ path, accountID, createdAt: Date.now() })) } catch { /* No cross-page restoration promise when storage is unavailable. */ }
}

export function consumeAuthReturnPath(accountID: number, isAdmin: boolean) {
  let raw: string | null = null
  try { raw = sessionStorage.getItem(key) } catch { return '' }
  clearAuthReturnPath()
  try {
    const saved = JSON.parse(raw || 'null')
    const age = Date.now() - saved?.createdAt
    return saved?.accountID === accountID && typeof saved.path === 'string' && allowed(saved.path, isAdmin)
      && Number.isFinite(age) && age >= 0 && age <= maxAgeMs ? saved.path as string : ''
  } catch { return '' }
}
