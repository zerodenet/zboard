import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createTicket, fetchTicket, fetchTickets, replyTicket, type TicketDetail, type TicketReferences } from '../api/client'
import { confirmAction } from '../utils/feedback'
import TicketCenter from './TicketCenter.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import TimeBadge from './TimeBadge.vue'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'

vi.mock('../api/client', () => ({ createTicket: vi.fn(), fetchTicket: vi.fn(), fetchTickets: vi.fn(), closeTicket: vi.fn(), replyTicket: vi.fn(), updateTicketStatus: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: 'verified', reauthRequired: false, sessionEpoch: 0, user: { id: 1 }, isAdmin: false }) }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn(async () => ({ revision: 1, page_available: true, nodes: [] })) }))
const modal = defineComponent({
  props: { open: Boolean }, emits: ['close'],
  setup(props, { slots, emit }) {
    return () => props.open ? h('section', { role: 'dialog' }, [slots.default?.(), slots.footer?.({ requestClose: () => emit('close') })]) : null
  },
})
function detail(references?: TicketReferences): TicketDetail {
  return {
    ticket: { id: 71, ticket_no: 'T-71', user_id: 1, user_email: 'member@example.test', subject: '原问题', category: 'billing', priority: 1, status: 'open', message_count: 1, last_message_at: '2026-10-20T00:00:00Z', created_at: '2026-10-20T00:00:00Z', updated_at: '2026-10-20T00:00:00Z', ...(references ? { references } : {}) },
    messages: [{ id: 1, ticket_id: 71, author_role: 'user', author_email: 'member@example.test', type: 'message', body: '原问题描述', from_status: '', to_status: '', created_at: '2026-10-20T00:00:00Z' }],
    has_older_messages: false,
  }
}

describe('ticket references and account context', () => {
  let wrapper: VueWrapper | undefined
  let router: Router
  beforeEach(() => {
    vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts()
    vi.mocked(fetchTickets).mockResolvedValue({ items: [], total: 0, offset: 0, limit: 25, page: { total: 0, offset: 0, limit: 25 } } as any)
    vi.mocked(fetchTicket).mockResolvedValue(detail())
    vi.mocked(createTicket).mockResolvedValue(detail())
    vi.mocked(replyTicket).mockResolvedValue(detail())
    vi.mocked(confirmAction).mockResolvedValue(true)
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; clearReauthenticationDrafts() })
  async function open(path = '/account/tickets', admin = false) {
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/account/tickets', component: TicketCenter, props: { admin } },
      { path: '/admin/tickets', component: TicketCenter, props: { admin } },
    ] })
    await router.push(path); await router.isReady()
    wrapper = mount(RouterView, { global: {
      plugins: [router], components: { UiButton, UiInput, UiSelect, UiTextarea, TimeBadge },
      stubs: { ModalDialog: modal, TicketAttachmentEditor: true, PageRefreshButton: true },
    } })
    await flushPromises()
  }
  async function content() {
    await wrapper!.get('#ticket-subject').setValue('需要核对账单')
    await wrapper!.get('#ticket-body').setValue('请核对这笔记录，附上发生时间和现象。')
  }
  async function submit() { await wrapper!.get('#create-ticket-form').trigger('submit'); await flushPromises() }
  async function create() { await wrapper!.findAll('button').find(button => button.text().includes('新建工单'))!.trigger('click'); await flushPromises() }

  it('restores ordinary ticket body as text without submitting or reuploading attachments', async () => {
    await open('/account/tickets?new=1&order_id=27'); await content()
    const body = '<img src=x onerror="alert(1)"> plain support draft'
    await wrapper!.get('#ticket-body').setValue(body)
    expect(captureReauthenticationDrafts(1, router.currentRoute.value.fullPath).ok).toBe(true)
    wrapper!.unmount(); wrapper = undefined
    confirmReauthenticationDraftIdentity(1, false)
    await open('/account/tickets?new=1&order_id=27')
    expect((wrapper!.get('#ticket-subject').element as HTMLInputElement).value).toBe('需要核对账单')
    expect((wrapper!.get('#ticket-body').element as HTMLTextAreaElement).value).toBe(body)
    expect(wrapper!.find('img').exists()).toBe(false)
    expect(createTicket).not.toHaveBeenCalled(); expect(replyTicket).not.toHaveBeenCalled()
  })
  it('restores an unsent reply only after its current ticket detail is fetched', async () => {
    await open('/account/tickets?ticket=71')
    await wrapper!.get('#ticket-reply-body').setValue('尚未发送的补充信息')
    expect(captureReauthenticationDrafts(1, router.currentRoute.value.fullPath).ok).toBe(true)
    wrapper!.unmount(); wrapper = undefined
    confirmReauthenticationDraftIdentity(1, false)
    await open('/account/tickets?ticket=71')
    expect(fetchTicket).toHaveBeenCalledWith(71, false, expect.any(Object))
    expect((wrapper!.get('#ticket-reply-body').element as HTMLTextAreaElement).value).toBe('尚未发送的补充信息')
    expect(replyTicket).not.toHaveBeenCalled(); expect(createTicket).not.toHaveBeenCalled()
  })
  it('keeps a recovered reply read-only if the fresh ticket was closed during OAuth', async () => {
    await open('/account/tickets?ticket=71'); await wrapper!.get('#ticket-reply-body').setValue('待核对的旧回复')
    expect(captureReauthenticationDrafts(1, router.currentRoute.value.fullPath).ok).toBe(true)
    wrapper!.unmount(); wrapper = undefined
    const closed = detail(); closed.ticket.status = 'closed'; vi.mocked(fetchTicket).mockResolvedValue(closed)
    confirmReauthenticationDraftIdentity(1, false)
    await open('/account/tickets?ticket=71')
    const recovered = wrapper!.get('[aria-label="已关闭工单的未发送草稿"]')
    expect((recovered.element as HTMLTextAreaElement).value).toBe('待核对的旧回复')
    expect(recovered.attributes()).toHaveProperty('readonly')
    expect(wrapper!.find('.ticket-reply').exists()).toBe(false)
    expect(replyTicket).not.toHaveBeenCalled()
  })
  it('prefills only context and requires explicit submission; sends one positive number per reference without impersonation', async () => {
    vi.mocked(createTicket).mockResolvedValue(detail({ order_id: 27, subscription_id: 4, wallet_entry_id: 8 }))
    await open('/account/tickets?new=1&order_id=27&subscription_id=4&wallet_entry_id=8&user_id=999')
    expect((wrapper!.get('#ticket-order_id').element as HTMLInputElement).value).toBe('27')
    expect((wrapper!.get('#ticket-subscription_id').element as HTMLInputElement).value).toBe('4')
    expect((wrapper!.get('#ticket-wallet_entry_id').element as HTMLInputElement).value).toBe('8')
    expect(createTicket).not.toHaveBeenCalled()
    await content(); await submit()
    expect(createTicket).toHaveBeenCalledWith({ subject: '需要核对账单', category: 'billing', priority: 1, body: '请核对这笔记录，附上发生时间和现象。', attachments: [], references: { order_id: 27, subscription_id: 4, wallet_entry_id: 8 } })
    expect(wrapper!.get('[aria-label="关联记录"]').text()).toContain('关联订单#27')
    expect(router.currentRoute.value.query).toEqual({ ticket: '71' })
  })
  it.each(['0', '-1', '1.5', '1e3', '9007199254740992', 'not-an-id'])('rejects invalid optional reference %s before creating', async invalid => {
    await open('/account/tickets?new=1')
    await content(); await wrapper!.get('#ticket-order_id').setValue(invalid); await submit()
    expect(wrapper!.text()).toContain('请输入有效的正整数编号')
    expect(createTicket).not.toHaveBeenCalled()
  })
  it('does not guess a target when query parameters repeat', async () => {
    await open('/account/tickets?new=1&order_id=27&order_id=28')
    await content(); await submit()
    expect(createTicket).not.toHaveBeenCalled()
    expect((wrapper!.get('#ticket-order_id').element as HTMLInputElement).value).toBe('27,28')
  })
  it('accepts the safe-integer boundary and sends decimal identifiers as numbers', async () => {
    await open('/account/tickets?new=1&order_id=00027&subscription_id=9007199254740991')
    await content(); await submit()
    expect(vi.mocked(createTicket).mock.calls[0]![0].references).toEqual({ order_id: 27, subscription_id: Number.MAX_SAFE_INTEGER })
  })
  it('omits references entirely for the ordinary no-reference creation flow', async () => {
    await open(); await create(); await content(); await submit()
    expect(createTicket).toHaveBeenCalledOnce()
    expect(vi.mocked(createTicket).mock.calls[0]![0]).not.toHaveProperty('references')
  })
  it('does not overwrite a dirty new-ticket draft when another context link changes the query', async () => {
    await open('/account/tickets?new=1&order_id=27')
    await content(); await wrapper!.get('#ticket-order_id').setValue('29')
    await router.push('/account/tickets?new=1&order_id=88&wallet_entry_id=9'); await flushPromises()
    expect((wrapper!.get('#ticket-subject').element as HTMLInputElement).value).toBe('需要核对账单')
    expect((wrapper!.get('#ticket-order_id').element as HTMLInputElement).value).toBe('29')
    expect((wrapper!.get('#ticket-wallet_entry_id').element as HTMLInputElement).value).toBe('')
    expect(wrapper!.text()).toContain('已保留当前新工单草稿')
    expect(createTicket).not.toHaveBeenCalled()
  })
  it.each(['/account/tickets?new=1&order_id=27', '/account/tickets?new=1&order_id=27&ticket=99'])('keeps a dirty reply on its original conversation when context arrives at %s', async path => {
    await open('/account/tickets?ticket=71')
    await wrapper!.get('#ticket-reply-body').setValue('旧工单尚未发送的回复')
    await router.push(path); await flushPromises()
    expect(wrapper!.find('#create-ticket-form').exists()).toBe(false)
    expect((wrapper!.get('#ticket-reply-body').element as HTMLTextAreaElement).value).toBe('旧工单尚未发送的回复')
    expect(wrapper!.text()).toContain('当前工单有未发送的回复')
    expect(wrapper!.get('.ticket-title-line').text()).toContain('T-71')
    expect(fetchTicket).toHaveBeenCalledOnce()
    expect(createTicket).not.toHaveBeenCalled()
    expect(replyTicket).not.toHaveBeenCalled()
    expect(confirmAction).not.toHaveBeenCalled()
    await wrapper!.get('.ticket-reply').trigger('submit'); await flushPromises()
    expect(replyTicket).toHaveBeenCalledWith(71, '旧工单尚未发送的回复', false, [])
    expect(createTicket).not.toHaveBeenCalled()
    expect(wrapper!.find('#create-ticket-form').exists()).toBe(false)
  })
  it('opens pending context only after explicit new-ticket confirmation and never carries an old reply to the new ticket', async () => {
    await open('/account/tickets?ticket=71')
    await wrapper!.get('#ticket-reply-body').setValue('不得带入新工单的旧回复')
    await router.push('/account/tickets?new=1&order_id=27'); await flushPromises()
    vi.mocked(confirmAction).mockResolvedValueOnce(false)
    await create()
    expect(wrapper!.find('#create-ticket-form').exists()).toBe(false)
    expect((wrapper!.get('#ticket-reply-body').element as HTMLTextAreaElement).value).toBe('不得带入新工单的旧回复')
    expect(createTicket).not.toHaveBeenCalled()
    await create()
    expect((wrapper!.get('#ticket-order_id').element as HTMLInputElement).value).toBe('27')
    expect((wrapper!.get('#ticket-reply-body').element as HTMLTextAreaElement).value).toBe('')
    const created = detail({ order_id: 27 }); created.ticket.id = 72; created.ticket.ticket_no = 'T-72'
    vi.mocked(createTicket).mockResolvedValue(created)
    await content(); await submit()
    expect(router.currentRoute.value.query.ticket).toBe('72')
    expect((wrapper!.get('#ticket-reply-body').element as HTMLTextAreaElement).value).toBe('')
    expect(replyTicket).not.toHaveBeenCalled()
    expect(createTicket).toHaveBeenCalledOnce()
  })
  it('allows the same context to open a later clean draft after a successful submission', async () => {
    await open('/account/tickets?new=1&order_id=27')
    await content(); await submit()
    expect(wrapper!.find('#create-ticket-form').exists()).toBe(false)
    await router.push('/account/tickets?new=1&order_id=27'); await flushPromises()
    expect((wrapper!.get('#ticket-order_id').element as HTMLInputElement).value).toBe('27')
    expect(createTicket).toHaveBeenCalledOnce()
  })
  it('keeps a rejected ownership reference editable and preserves the unsent description', async () => {
    vi.mocked(createTicket).mockRejectedValue({ response: { data: { message: '仅可关联当前账户的记录。', error: { version: 1, code: 'validation_failed', fields: { 'references.order_id': '订单不属于当前账户。' } } } } })
    await open('/account/tickets?new=1&order_id=27'); await content(); await submit()
    expect(wrapper!.text()).toContain('订单不属于当前账户。')
    expect((wrapper!.get('#ticket-body').element as HTMLTextAreaElement).value).toContain('请核对这笔记录')
    expect(router.currentRoute.value.query.ticket).toBeUndefined()
    await wrapper!.get('#ticket-order_id').setValue('')
    expect(wrapper!.text()).not.toContain('订单不属于当前账户。')
    expect(createTicket).toHaveBeenCalledOnce()
  })
  it('does not invent references for old ticket details', async () => {
    await open('/account/tickets?ticket=71')
    expect(wrapper!.text()).toContain('原问题描述')
    expect(wrapper!.find('[aria-label="关联记录"]').exists()).toBe(false)
  })
  it('shows only actual persisted detail references, independent of route hints', async () => {
    vi.mocked(fetchTicket).mockResolvedValue(detail({ subscription_id: 4 }))
    await open('/account/tickets?ticket=71&order_id=99')
    const records = wrapper!.get('[aria-label="关联记录"]')
    expect(records.text()).toContain('关联订阅#4')
    expect(records.text()).not.toContain('关联订单')
    expect(records.text()).not.toContain('99')
  })
  it('ignores account-create query hints in administrative ticket handling', async () => {
    await open('/admin/tickets?new=1&order_id=27&user_id=999', true)
    expect(wrapper!.find('#create-ticket-form').exists()).toBe(false)
    expect(wrapper!.findAll('button').some(button => button.text().includes('新建工单'))).toBe(false)
    expect(createTicket).not.toHaveBeenCalled()
    expect(fetchTickets).toHaveBeenCalledWith(expect.anything(), true, expect.anything())
  })
})
