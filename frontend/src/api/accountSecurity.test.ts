import { vi, it, expect, beforeEach, afterEach } from 'vitest'
const mocks = vi.hoisted(() => ({ login: 'session-a', post: vi.fn() }))
vi.mock('./client', () => ({ getAuthToken: () => mocks.login, authenticatedAPI: { post: mocks.post } }))
import { accountConfirmation, confirmAccountPassword, clearAccountConfirmation, changeAccountPassword } from './accountSecurity'
beforeEach(() => { clearAccountConfirmation(); vi.useFakeTimers(); mocks.login = 'session-a'; mocks.post.mockResolvedValue({ data: { data: { confirmation: 'proof', expires_at: Date.now() / 1000 + 300 } } }) })
afterEach(() => vi.useRealTimers())
it('reuses proof until expiry and clears it on account changes', async () => {
 await confirmAccountPassword('password'); expect(accountConfirmation()).toBe('proof'); vi.advanceTimersByTime(300000); expect(accountConfirmation()).toBe('')
 mocks.post.mockResolvedValue({ data: { data: { confirmation: 'proof', expires_at: Date.now() / 1000 + 300 } } }); await confirmAccountPassword('password'); mocks.login='session-b';expect(accountConfirmation()).toBe('')
})
it('clears proof after changing the password', async () => { await confirmAccountPassword('password'); await changeAccountPassword('password','new-password'); expect(accountConfirmation()).toBe('') })
