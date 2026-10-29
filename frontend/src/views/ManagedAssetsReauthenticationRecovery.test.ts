import { defineComponent, h, reactive } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import ManagedDNS from './ManagedDNS.vue'
import Certificates from './Certificates.vue'
import FormField from '../components/FormField.vue'
import { createManagedCertificate, createManagedDNSRecord, fetchManagedCertificatesPage, fetchManagedDNSRecordsPage, fetchNodeAddressCandidates, fetchNodesPage, fetchProviderAccounts, issueManagedCertificate, renewManagedCertificate, syncManagedDNSRecord, updateManagedCertificate, updateManagedCertificateRenewal, updateManagedDNSRecord } from '../api/client'
import { fetchNavigation } from '../api/menus'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn(async () => ({ page_available: true, revision: 1, nodes: [] })) }))
vi.mock('../api/client', () => ({ createManagedCertificate: vi.fn(), createManagedDNSRecord: vi.fn(), deleteManagedCertificate: vi.fn(), deleteManagedDNSRecord: vi.fn(), fetchManagedCertificatesPage: vi.fn(), fetchManagedDNSRecordsPage: vi.fn(), fetchNodeAddressCandidates: vi.fn(), fetchNodesPage: vi.fn(), fetchProviderAccounts: vi.fn(), issueManagedCertificate: vi.fn(), renewManagedCertificate: vi.fn(), syncManagedDNSRecord: vi.fn(), updateManagedCertificate: vi.fn(), updateManagedCertificateRenewal: vi.fn(), updateManagedDNSRecord: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn(async () => true) }))
const provider = { id: 1, provider_key: 'cloudflare', name: 'DNS account', capabilities: ['dns.records'], status: 'active', revision: 7, credential_prefix: 'DO-NOT-CAPTURE-PREFIX', api_token: 'DO-NOT-CAPTURE-TOKEN' }
const dns = { id: 5, provider_account_id: 1, provider_name: 'DNS account', provider_key: 'cloudflare', node_id: 1, node_name: 'Node', domain_name: 'edge.example.test', record_type: 'A', record_value: '1.1.1.1', ttl: 60, proxied: false, revision: 4, status: 'active', public_resolved: true, last_error: '', latest_operation: undefined }
const certificate = { id: 9, node_id: 1, provider_account_id: 1, node_name: 'Node', name: 'Server edge', domains: ['edge.example.test'], contact_email: 'operator@example.test', challenge_type: 'dns-01', webroot_path: '', environment: 'staging', auto_renew: true, renew_before_days: 30, revision: 6, status: 'active', last_error: '', not_after: '2030-01-01T00:00:00Z', usage_count: 0 }
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const number = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', Number((event.target as HTMLInputElement).value)) }) } })
const toggle = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { type: 'checkbox', checked: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked) }) } })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined
let captured = ''
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts(); captured = ''
  state.app = reactive({ token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 })
  vi.mocked(fetchNavigation).mockResolvedValue({ page_available: true, revision: 1, nodes: [] } as any)
  vi.mocked(fetchProviderAccounts).mockResolvedValue([structuredClone(provider)] as any)
  vi.mocked(fetchNodesPage).mockResolvedValue({ items: [{ id: 1, lifecycle_status: 'active', ssh_password: 'DO-NOT-CAPTURE-SSH' }], total: 1 } as any)
  vi.mocked(fetchManagedDNSRecordsPage).mockResolvedValue({ items: [structuredClone(dns)], total: 1 } as any)
  vi.mocked(fetchManagedCertificatesPage).mockResolvedValue({ items: [structuredClone(certificate)], total: 1 } as any)
  vi.mocked(fetchNodeAddressCandidates).mockResolvedValue({ node_id: 1, policy: 'public_only', ipv4: [], ipv6: [], recommended_ipv4: '9.9.9.9', ssh_probe_status: 'not_configured' })
  vi.mocked(createManagedCertificate).mockResolvedValue(structuredClone(certificate) as any)
  vi.mocked(createManagedDNSRecord).mockResolvedValue({})
  vi.mocked(issueManagedCertificate).mockResolvedValue({ id: 10, status: 'running' } as any)
  vi.mocked(updateManagedCertificate).mockResolvedValue({})
  vi.mocked(updateManagedCertificateRenewal).mockResolvedValue({})
  vi.mocked(updateManagedDNSRecord).mockResolvedValue({} as any)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open(kind: 'dns' | 'cert') {
  const path = kind === 'dns' ? '/admin/dns-records' : '/admin/certificates'
  const component = kind === 'dns' ? ManagedDNS : Certificates
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component }] })
  await router.push(path)
  view = shallowMount(component, { global: { plugins: [router], renderStubDefaultSlot: true,
    components: { UiInput: input, UiTextarea: input, UiNumberInput: number, UiButton: defineComponent({ template: '<button><slot /></button>' }) },
    stubs: { UiInput: false, UiTextarea: false, UiNumberInput: false, UiButton: false, UiSelect: number, UiCheckbox: toggle, NodeLookup: number, FormField, ModalDialog: modal, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { template: '<aside><slot /></aside>' } },
  } })
  await flushPromises()
  return path
}
function allStored() { return Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('') }
async function roundTrip(kind: 'dns' | 'cert', path: string) {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  captured = allStored()
  view!.unmount(); view = undefined
  confirmReauthenticationDraftIdentity(1, true)
  await open(kind)
}
function expectNoWrites() {
  for (const request of [createManagedCertificate, createManagedDNSRecord, issueManagedCertificate, renewManagedCertificate, syncManagedDNSRecord, updateManagedCertificate, updateManagedCertificateRenewal, updateManagedDNSRecord]) expect(request).not.toHaveBeenCalled()
}
it('restores DNS text, provider/node selection and TTL while clearing takeover approval and never probing SSH on recovery', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openCreateDNS()
  Object.assign(vm.dnsForm, { provider_account_id: 1, node_id: 1, domain_name: 'unsaved.example.test', ttl: 120, takeover_existing: true, api_token: 'DO-NOT-CAPTURE-DNS' })
  await flushPromises()
  vm.dnsForm.ipv4_value = '8.8.8.8'; vm.markCreateAddressEdited('ipv4')
  const addressReads = vi.mocked(fetchNodeAddressCandidates).mock.calls.length
  await roundTrip('dns', path)
  const restored = view!.vm as any
  expect(restored.dnsOpen).toBe(true)
  expect(restored.dnsForm).toMatchObject({ provider_account_id: 1, node_id: 1, domain_name: 'unsaved.example.test', ipv4_value: '8.8.8.8', ttl: 120, takeover_existing: false })
  expect(captured).not.toContain('DO-NOT-CAPTURE')
  expect(captured).not.toContain('takeover_existing')
  expect(vi.mocked(fetchNodeAddressCandidates).mock.calls.length).toBe(addressReads)
  expectNoWrites()
  await restored.createDNS(); await flushPromises()
  expect(createManagedDNSRecord).toHaveBeenCalledWith(expect.objectContaining({ takeover_existing: false, domain_name: 'unsaved.example.test' }))
})
it('keeps the DNS original revision and baseline across two recoveries and blocks programmatic submit on conflict', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openEdit(structuredClone(dns)); vm.editForm.record_value = '8.8.8.8'
  vi.mocked(fetchManagedDNSRecordsPage).mockResolvedValue({ items: [{ ...dns, record_value: '2.2.2.2', revision: 5 }], total: 1 } as any)
  await roundTrip('dns', path); await roundTrip('dns', path)
  const restored = view!.vm as any
  expect(restored.editForm.record_value).toBe('8.8.8.8')
  expect(restored.editForm.revision).toBe(4)
  expect(restored.editDraftBaseline.record_value).toBe('1.1.1.1')
  expect(view!.text()).toContain('不能直接保存或同步')
  await restored.saveEdit()
  expectNoWrites()
})
it('requires a fresh matching DNS provider version through recapture', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openCreateDNS(); Object.assign(vm.dnsForm, { provider_account_id: 1, node_id: 1, domain_name: 'draft.example.test', ipv4_value: '8.8.8.8' })
  vi.mocked(fetchProviderAccounts).mockResolvedValue([{ ...provider, revision: 8 }] as any)
  await roundTrip('dns', path); await roundTrip('dns', path)
  const restored = view!.vm as any
  expect(restored.dnsForm.domain_name).toBe('draft.example.test')
  expect(restored.createProviderBaseline.revision).toBe(7)
  expect(restored.createRecoveryBlocked).toBe(true)
  await restored.createDNS()
  expectNoWrites()
})
it('submits a recovered DNS edit only after explicit action with its original expected_revision', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openEdit(structuredClone(dns)); vm.editForm.record_value = '8.8.8.8'
  await roundTrip('dns', path)
  expectNoWrites()
  await (view!.vm as any).saveEdit(); await flushPromises()
  expect(updateManagedDNSRecord).toHaveBeenCalledWith(5, expect.objectContaining({ record_value: '8.8.8.8', expected_revision: 4 }))
})
it('restores certificate application fields without keys, credentials, approval or automatic issue', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openCreate()
  Object.assign(vm.createForm, { node_id: 1, provider_account_id: 1, name: 'Unsubmitted edge', domains: 'new.example.test', contact_email: 'operator@example.test', environment: 'staging', auto_renew: false, renew_before_days: 10, private_key: 'DO-NOT-CAPTURE-PRIVATE', acme_credentials: 'DO-NOT-CAPTURE-ACME', issue_approved: true })
  await roundTrip('cert', path)
  const restored = view!.vm as any
  expect(restored.createForm).toMatchObject({ name: 'Unsubmitted edge', domains: 'new.example.test', node_id: 1, provider_account_id: 1, environment: 'staging', auto_renew: false, renew_before_days: 10 })
  expect(captured).not.toContain('DO-NOT-CAPTURE')
  expect(captured).not.toContain('issue_approved')
  expectNoWrites()
  await restored.create(); await flushPromises()
  expect(createManagedCertificate).toHaveBeenCalledTimes(1)
  expect(issueManagedCertificate).toHaveBeenCalledTimes(1)
})
it('preserves conflicting certificate edit revision and ordinary baseline through recapture without overwriting', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openEdit(structuredClone(certificate)); vm.editForm.name = 'Old draft name'
  vi.mocked(fetchManagedCertificatesPage).mockResolvedValue({ items: [{ ...certificate, name: 'Concurrent edit', revision: 7 }], total: 1 } as any)
  await roundTrip('cert', path); await roundTrip('cert', path)
  const restored = view!.vm as any
  expect(restored.editForm.name).toBe('Old draft name')
  expect(restored.editForm.revision).toBe(6)
  expect(restored.editDraftBaseline.fields.name).toBe('Server edge')
  await restored.saveEdit()
  expectNoWrites()
})
it('preserves an unsubmitted renewal policy and allows explicit disable without requiring a working provider', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openRenewal(structuredClone(certificate)); vm.renewalForm.auto_renew = false; vm.renewalForm.renew_before_days = 10
  vi.mocked(fetchProviderAccounts).mockResolvedValue([{ ...provider, status: 'disabled' }] as any)
  await roundTrip('cert', path)
  const restored = view!.vm as any
  expect(restored.renewalForm).toMatchObject({ auto_renew: false, renew_before_days: 10, revision: 6 })
  expectNoWrites()
  await restored.saveRenewal(); await flushPromises()
  expect(updateManagedCertificateRenewal).toHaveBeenCalledWith(9, { auto_renew: false, renew_before_days: 10, expected_revision: 6 })
  expect(issueManagedCertificate).not.toHaveBeenCalled(); expect(renewManagedCertificate).not.toHaveBeenCalled()
})
it('does not restore persisted running-operation state and blocks old edits against a fresh running certificate', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openEdit(structuredClone(certificate)); vm.editForm.name = 'My draft'; vm.editForm.issue_inflight = true
  vi.mocked(fetchManagedCertificatesPage).mockResolvedValue({ items: [{ ...certificate, status: 'renewing', latest_operation: { status: 'running' } }], total: 1 } as any)
  await roundTrip('cert', path)
  const restored = view!.vm as any
  expect(restored.editForm.name).toBe('My draft')
  expect(captured).not.toContain('issue_inflight'); expect(captured).not.toContain('latest_operation')
  expect(restored.editRecoveryBlocked).toBe(true)
  await restored.saveEdit()
  expectNoWrites()
})
it('retains the stored DNS draft when a fresh read fails and recovers only after an explicit successful retry', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openEdit(structuredClone(dns)); vm.editForm.record_value = '8.8.8.8'
  vi.mocked(fetchProviderAccounts).mockRejectedValue(new Error('network unavailable'))
  await roundTrip('dns', path)
  expect(allStored()).toContain('8.8.8.8')
  expect((view!.vm as any).editOpen).toBe(false)
  vi.mocked(fetchProviderAccounts).mockResolvedValue([structuredClone(provider)] as any)
  await (view!.vm as any).editDraftRecovery.restore(); await flushPromises()
  expect((view!.vm as any).editForm.record_value).toBe('8.8.8.8')
  expectNoWrites()
})
it('clears a same-account draft when fresh page permission is revoked', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openEdit(structuredClone(certificate)); vm.editForm.name = 'Private draft'
  vi.mocked(fetchNavigation).mockResolvedValue({ page_available: false, revision: 2, nodes: [] } as any)
  await roundTrip('cert', path)
  expect((view!.vm as any).editOpen).toBe(false)
  expect(allStored()).toBe('')
  expectNoWrites()
})
it('does not restore or submit a previous-account draft after identity changes', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openEdit(structuredClone(certificate)); vm.editForm.name = 'Previous account'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  state.app.user.id = 2; state.app.sessionEpoch++
  confirmReauthenticationDraftIdentity(2, true)
  expect(vm.editOpen).toBe(false)
  expect(vm.editForm.name).toBe('')
  await vm.saveEdit()
  expectNoWrites()
  view!.unmount(); view = undefined
  await open('cert')
  expect((view!.vm as any).editOpen).toBe(false)
  expect(allStored()).toBe('')
})
it('does not replace a newly opened editor when a previous asynchronous recovery finishes', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openEdit(structuredClone(dns)); vm.editForm.record_value = '8.8.8.8'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true)
  let finish!: (value: any) => void, calls = 0
  vi.mocked(fetchProviderAccounts).mockImplementation(() => ++calls === 2 ? new Promise<any>(resolve => { finish = resolve }) : Promise.resolve([structuredClone(provider)] as any))
  await open('dns')
  const current = view!.vm as any
  current.openCreateDNS(); current.dnsForm.domain_name = 'New local input'
  finish([structuredClone(provider)]); await flushPromises()
  expect(current.dnsForm.domain_name).toBe('New local input')
  expect(current.editOpen).toBe(false)
  expect(allStored()).toContain('8.8.8.8')
  expectNoWrites()
})

it('keeps DNS create text but blocks a selected node missing from the fresh permission-scoped read', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openCreateDNS(); Object.assign(vm.dnsForm, { provider_account_id: 1, node_id: 1, domain_name: 'lost-node.example.test', ipv4_value: '8.8.8.8' })
  vi.mocked(fetchNodesPage).mockResolvedValue({ items: [], total: 0 } as any)
  await roundTrip('dns', path)
  const restored = view!.vm as any
  expect(restored.dnsForm.domain_name).toBe('lost-node.example.test')
  expect(restored.createRecoveryBlocked).toBe(true)
  await restored.createDNS()
  expectNoWrites()
})
it('does not adopt a new certificate policy revision or run renewal during conflicting recovery', async () => {
  const path = await open('cert'), vm = view!.vm as any
  vm.openRenewal(structuredClone(certificate)); vm.renewalForm.auto_renew = false
  vi.mocked(fetchManagedCertificatesPage).mockResolvedValue({ items: [{ ...certificate, revision: 7, renew_before_days: 5 }], total: 1 } as any)
  await roundTrip('cert', path); await roundTrip('cert', path)
  const restored = view!.vm as any
  expect(restored.renewalForm).toMatchObject({ auto_renew: false, revision: 6 })
  expect(restored.renewalDraftBaseline.fields.renew_before_days).toBe(30)
  expect(restored.renewalState.dirty.value).toBe(true)
  await restored.saveRenewal()
  expectNoWrites()
})
it('fences a pending fresh-resource response after the account changes', async () => {
  const path = await open('dns'), vm = view!.vm as any
  vm.openEdit(structuredClone(dns)); vm.editForm.record_value = '8.8.8.8'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true)
  let finish!: (value: any) => void, calls = 0
  vi.mocked(fetchProviderAccounts).mockImplementation(() => ++calls === 2 ? new Promise<any>(resolve => { finish = resolve }) : Promise.resolve([structuredClone(provider)] as any))
  await open('dns')
  state.app.user.id = 2; state.app.sessionEpoch++
  confirmReauthenticationDraftIdentity(2, true)
  finish([structuredClone(provider)]); await flushPromises()
  expect((view!.vm as any).editOpen).toBe(false)
  expect((view!.vm as any).editForm.record_value).toBe('')
  expect(allStored()).toBe('')
  expectNoWrites()
})
