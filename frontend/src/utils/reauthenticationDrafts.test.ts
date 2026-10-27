import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { approveReauthenticationDraftPage, captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity, hasReauthenticationDraft, REAUTH_DRAFT_MANIFEST, REAUTH_DRAFT_TTL_MS, registerReauthenticationDraft, safeReauthenticationRoute, takeReauthenticationDraft } from './reauthenticationDrafts'
import { ticketRecoveryDraft } from './reauthenticationDraftSchemas'
const path = '/account/tickets?page=2#reply'
const make = () => ({ subject: '连接问题', category: 'connection', priority: 1, body: '尚未发送的普通问题描述', references: { order_id: '27', subscription_id: '', wallet_entry_id: '' }, password: 'excluded-password', token: 'excluded-token', private_key: 'excluded-key', quote_fingerprint: 'excluded-quote', attachments: [{ url: 'excluded-attachment' }] })
let stops: (() => unknown)[] = []
beforeEach(() => { sessionStorage.clear(); clearReauthenticationDrafts() })
afterEach(() => { for (const stop of stops) stop(); stops = []; clearReauthenticationDrafts(); vi.restoreAllMocks() })
function register(scope = 'account.tickets.create') { stops.push(registerReauthenticationDraft({ actorID: 1, scope, permission: 'account', route: () => path, read: make, sanitize: ticketRecoveryDraft })) }
it('projects explicit fields and refuses payload reads before identity and permission verification', () => {
  register(); expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  const raw = Array.from({ length: sessionStorage.length }, (_, i) => sessionStorage.getItem(sessionStorage.key(i)!)).join('')
  expect(raw).toContain('尚未发送的普通问题描述')
  for (const secret of ['excluded-password', 'excluded-token', 'excluded-key', 'excluded-quote', 'excluded-attachment']) expect(raw).not.toContain(secret)
  const reads = vi.spyOn(sessionStorage, 'getItem')
  expect(takeReauthenticationDraft(1, 'account.tickets.create', path, 'account', ticketRecoveryDraft)).toBeUndefined()
  expect(reads.mock.calls).toHaveLength(0)
  confirmReauthenticationDraftIdentity(1, false)
  expect(takeReauthenticationDraft(1, 'account.tickets.create', path, 'account', ticketRecoveryDraft)).toBeUndefined()
  expect(reads.mock.calls.filter(([key]) => key.startsWith('zboard.reauth-draft.v1.'))).toHaveLength(0)
  approveReauthenticationDraftPage(1, path, true)
  expect(takeReauthenticationDraft(1, 'account.tickets.create', path, 'account', ticketRecoveryDraft)?.body).toBe(make().body)
  expect(reads.mock.calls.some(([key]) => key.startsWith('zboard.reauth-draft.v1.'))).toBe(true)
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull()
})
it.each(['other-account', 'expired', 'permission-denied', 'future-clock'] as const)('clears inaccessible recovery for %s', reason => {
  register(); captureReauthenticationDrafts(1, path)
  if (reason === 'expired') vi.spyOn(Date, 'now').mockReturnValue(Date.now() + REAUTH_DRAFT_TTL_MS + 1)
  if (reason === 'future-clock') vi.spyOn(Date, 'now').mockReturnValue(Date.now() - 1000)
  confirmReauthenticationDraftIdentity(reason === 'other-account' ? 2 : 1, false)
  if (reason === 'permission-denied') approveReauthenticationDraftPage(1, path, false)
  expect(hasReauthenticationDraft(1, 'account.tickets.create', path, 'account')).toBe(false)
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull()
})
it('binds exact route, actor and scope; sanitizes route credentials and payment context', () => {
  expect(safeReauthenticationRoute('/account/plans?plan=2&sku=5&token=secret&quote_fingerprint=old&checkout_intent=old&payment_authorization=old')).toBe('/account/plans?plan=2&sku=5')
  register(); captureReauthenticationDrafts(1, path); confirmReauthenticationDraftIdentity(1, false); approveReauthenticationDraftPage(1, path, true)
  expect(takeReauthenticationDraft(1, 'account.tickets.reply', path, 'account', ticketRecoveryDraft)).toBeUndefined()
  expect(takeReauthenticationDraft(1, 'account.tickets.create', '/account/tickets?ticket=999', 'account', ticketRecoveryDraft)).toBeUndefined()
  expect(hasReauthenticationDraft(1, 'account.tickets.create', path, 'account')).toBe(true)
})
it('rejects per-draft and aggregate limits without retaining a partial set', () => {
  stops.push(registerReauthenticationDraft({ actorID: 1, scope: 'test.large', permission: 'account', route: () => path, read: () => ({ body: 'x'.repeat(70_000) }), sanitize: value => value as object }))
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  stops.pop()!()
  for (let i = 0; i < 4; i++) stops.push(registerReauthenticationDraft({ actorID: 1, scope: `test.total-${i}`, permission: 'account', route: () => path, read: () => ({ body: 'x'.repeat(60_000) }), sanitize: value => value as object }))
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull()
})
it('handles storage denial without losing the mounted source form', () => {
  const source = make()
  stops.push(registerReauthenticationDraft({ actorID: 1, scope: 'account.tickets.create', permission: 'account', route: () => path, read: () => source, sanitize: ticketRecoveryDraft }))
  // happy-dom binds Storage methods as instance-owned values on first use.
  // Intercept the actual receiver, and prove the fault was really exercised.
  const writes = vi.spyOn(sessionStorage, 'setItem').mockImplementation(() => { throw new DOMException('quota', 'QuotaExceededError') })
  const result = captureReauthenticationDrafts(1, path)
  expect(writes).toHaveBeenCalledOnce()
  expect(writes).toHaveBeenCalledWith(expect.stringMatching(/^zboard.reauth-draft.v1./), expect.stringContaining(source.body))
  expect(result.ok).toBe(false)
  expect(result.message).toContain('尚未离开此页')
  expect(source.body).toBe('尚未发送的普通问题描述')
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull()
})
