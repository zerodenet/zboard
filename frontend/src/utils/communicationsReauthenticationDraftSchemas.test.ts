import { describe, expect, it } from 'vitest'
import { announcementRecoveryDraft, menuRecoveryDraft, taskRecoveryDraft } from './communicationsReauthenticationDraftSchemas'
const fields = { title: 'Draft', content: 'Body', severity: 'info', audience: 'user', popup_enabled: true, dismissible: true, starts_at: '2026-10-27T08:00', ends_at: '' }
const tree = { revision: 7, nodes: [{ id: 'core:tasks', parent_id: '', label: 'Tasks', icon: 'tasks', path: '/admin/tasks', position: 1, hidden: false }] }
describe('communication editor recovery allowlists', () => {
  it('keeps announcement ordinary fields and original revision without publication or delivery state', () => {
    const extra = { status: 'published', approved: true, sent_at: 'yesterday', secret: 'DO-NOT-STORE' }
    expect(announcementRecoveryDraft({ id: 1, revision: 4, fields: { ...fields, ...extra }, baseline: { ...fields, ...extra }, ...extra })).toEqual({ id: 1, revision: 4, fields, baseline: fields })
    expect(announcementRecoveryDraft({ id: 1, revision: 4, fields, baseline: null })).toBeUndefined()
    expect(announcementRecoveryDraft({ id: 0, revision: 4, fields, baseline: null })).toBeUndefined()
    expect(announcementRecoveryDraft({ id: 0, revision: 0, fields: { ...fields, content: [] }, baseline: null })).toBeUndefined()
    expect(announcementRecoveryDraft({ id: 0, revision: 0, fields, baseline: null })).toEqual({ id: 0, revision: 0, fields, baseline: null })
  })
  it('keeps presentation tree and its baseline without restoring page authority or plugin state', () => {
    const extra = { owner: 'plugin', plugin_id: 'x', page_id: 'p', condition: 'authority', secret: 'DO-NOT-STORE' }
    const result = menuRecoveryDraft({ surface: 'admin', tree: { ...tree, nodes: [{ ...tree.nodes[0], ...extra }] }, baseline: tree, page_available: true })
    expect(result).toEqual({ surface: 'admin', tree, baseline: tree })
    expect(menuRecoveryDraft({ surface: 'admin', tree: { ...tree, revision: 8 }, baseline: tree })).toBeUndefined()
    expect(menuRecoveryDraft({ surface: 'root', tree, baseline: tree })).toBeUndefined()
    expect(menuRecoveryDraft({ surface: 'admin', tree: { ...tree, nodes: [tree.nodes[0], tree.nodes[0]] }, baseline: tree })).toBeUndefined()
  })
  it('restores active ordinary email content and explicit targets, never execution or financial authorization', () => {
    const input = { type: 'email', max_attempts: 3, scope_mode: 'users', scope_ids: [8, 2], email: { subject: 'Support', body: 'Unsubmitted text', secret: 'DO-NOT-STORE' }, quota: { delta_mb: 123, reason: 'inactive' }, auto_run: true, approved: true, executed: true, status: 2, task_id: 9, delivery_state: 'accepted', quote: 'DO-NOT-STORE', debit_authorization: 'DO-NOT-STORE' }
    expect(taskRecoveryDraft(input)).toEqual({ type: 'email', max_attempts: 3, scope_mode: 'users', scope_ids: [8, 2], quota: null, email: { subject: 'Support', body: 'Unsubmitted text' } })
    expect(taskRecoveryDraft({ ...input, type: 'node_system_action' })).toBeUndefined()
    expect(taskRecoveryDraft({ ...input, scope_mode: 'subscriptions' })).toBeUndefined()
    expect(taskRecoveryDraft({ ...input, scope_ids: [8, 8] })).toBeUndefined()
    expect(taskRecoveryDraft({ ...input, scope_ids: [-1] })).toBeUndefined()
    expect(taskRecoveryDraft({ ...input, scope_mode: 'all' })).toBeUndefined()
  })
  it('keeps unsubmitted quota input without a completed adjustment or hidden email body', () => {
    const draft = taskRecoveryDraft({ type: 'quota', max_attempts: 3, scope_mode: 'subscriptions', scope_ids: [4], quota: { delta_mb: -5, reason: 'Operator draft', idempotency_key: 'DO-NOT-STORE' }, email: { body: 'DO-NOT-STORE' }, adjustment_id: 18, auto_run: true })
    expect(draft).toEqual({ type: 'quota', max_attempts: 3, scope_mode: 'subscriptions', scope_ids: [4], quota: { delta_mb: -5, reason: 'Operator draft' }, email: null })
    expect(JSON.stringify(draft)).not.toContain('DO-NOT-STORE')
  })
})
