import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { me } from '../api/client'
import { useAppStore } from './app'
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), me: vi.fn() }))
beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()) })
describe('account identity refresh', () => {
  it('retains login and identity when the server cannot be reached', async () => {
    const app = useAppStore()
    app.setToken('current')
    app.setUser({ id: 1, email: 'admin@example.test', is_admin: true })
    vi.mocked(me).mockRejectedValueOnce(new Error('offline'))
    await expect(app.loadMe()).rejects.toThrow('offline')
    expect(app.isAuthenticated).toBe(true)
    expect(app.user.email).toBe('admin@example.test')
    expect(localStorage.getItem('zboard.auth.token')).toBe('current')
  })
  it('does not overwrite the new identity with a late response from the previous login', async () => {
    const app = useAppStore()
    app.setToken('old')
    let finish!: (user: unknown) => void
    vi.mocked(me).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const pending = app.loadMe()
    app.setToken('new')
    app.setUser({ id: 2, email: 'new@example.test' })
    finish({ id: 1, email: 'old@example.test', is_admin: true })
    await pending
    expect(app.user.email).toBe('new@example.test')
    expect(app.isAdmin).toBe(false)
  })
})
