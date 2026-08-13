import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const mainSource = readFileSync(resolve(process.cwd(), 'src/main.ts'), 'utf8')
const formStateSource = readFileSync(resolve(process.cwd(), 'src/composables/useFormState.ts'), 'utf8')

describe('expired session navigation', () => {
  it('redirects mounted protected routes back to login with their current location', () => {
    expect(mainSource).toContain('window.addEventListener(AUTH_SESSION_EXPIRED_EVENT')
    expect(mainSource).toContain('if (!current.meta.requiresAuth)')
    expect(mainSource).toContain("router.replace({ path: '/login', query: { redirect: current.fullPath } })")
    expect(mainSource).toContain('.finally(resetAuthSessionExpired)')
  })

  it('does not let dirty-form guards trap an already expired session', () => {
    expect(formStateSource).toContain('if (isAuthSessionExpired()) return true')
    expect(formStateSource).toContain('if (isAuthSessionExpired() || !dirty.value) return')
  })
})
