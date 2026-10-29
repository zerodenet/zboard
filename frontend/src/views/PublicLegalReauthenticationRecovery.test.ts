import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Settings from './Settings.vue'
import FormField from '../components/FormField.vue'
import { fetchSystemConfigs, updateSystemConfig, updateSiteSettings, testSMTP } from '../api/client'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn(async () => ({ page_available: true, revision: 1, nodes: [] })) }))
vi.mock('../api/client', () => ({ fetchSystemConfigs: vi.fn(), updateSystemConfig: vi.fn(), updateSiteSettings: vi.fn(), testSMTP: vi.fn(), fetchEmailTemplates: vi.fn(), createEmailTemplate: vi.fn(), deleteEmailTemplate: vi.fn(), previewEmailTemplate: vi.fn(), updateEmailTemplate: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn(async () => true) }))
const policy = [{ slug: 'terms', title: 'Terms', summary: '', content: '# Server terms', published: false, placements: ['footer', 'purchase'] }]
const legal = [{ label: 'Company No.', value: '123', url: 'https://registry.example' }]
const config = (key: string, value: unknown, revision = 1) => ({ id: 1, config_key: key, name: key, value, revision, value_type: key === 'site_policy_documents' || key === 'site_legal_items' ? 'json' : 'string', is_public: key.startsWith('site_'), is_secret: key === 'smtp_password', configured: key === 'smtp_password', description: '', updated_at: '' })
const rows = () => [config('site_policy_documents', structuredClone(policy)), config('site_legal_items', structuredClone(legal)), config('site_terms_content', 'Old legacy terms'), config('smtp_password', '')]
let view: VueWrapper | undefined
let captured = ''
const path = '/admin/settings/legal'
beforeEach(() => {
  vi.clearAllMocks(); clearReauthenticationDrafts(); sessionStorage.clear(); captured = ''
  state.app = { token: 'verified', user: { id: 1, email: 'admin@example.test' }, reauthRequired: false, sessionEpoch: 0, siteName: 'Server', loadSetupStatus: vi.fn(async () => ({ site_name: 'Server', site_url: 'https://panel.example', allow_registration: false })), loadPublicConfigs: vi.fn() }
  vi.mocked(fetchSystemConfigs).mockResolvedValue(rows() as any)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component: Settings, meta: { settingsSection: 'legal' } }] })
  await router.push(path)
  view = shallowMount(Settings, { global: { plugins: [router], renderStubDefaultSlot: true, components: { UiButton: defineComponent({ template: '<button><slot /></button>' }) }, stubs: { PolicyDocumentsEditor: false, LegalItemsEditor: false, UiInput: false, UiTextarea: false, UiButton: false, Button: defineComponent({ props: { disabled: Boolean }, template: '<button :disabled="disabled"><slot /></button>' }), FormField, PageAlert: { template: '<aside><slot /></aside>' } } } })
  await flushPromises()
}
function stored() { return Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('') }
async function roundTrip() {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true); captured = stored()
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true)
  await open()
}
it('restores both real editors and ordinary legacy text verbatim without saving or storing SMTP drafts', async () => {
  await open()
  await view!.get('#policy-document-0-content').setValue('# Unsaved policy\n\nFull ordinary body.')
  await view!.get('#legal-item-0-value').setValue('Unsubmitted legal value')
  const vm = view!.vm as any
  vm.drafts.site_terms_content = 'Unsubmitted fallback terms'; vm.drafts.smtp_password = 'MUST-NOT-CAPTURE'
  const policyText = vm.drafts.site_policy_documents, legalText = vm.drafts.site_legal_items
  await roundTrip()
  expect((view!.vm as any).drafts.site_policy_documents).toBe(policyText)
  expect((view!.vm as any).drafts.site_legal_items).toBe(legalText)
  expect((view!.vm as any).drafts.site_terms_content).toBe('Unsubmitted fallback terms')
  expect((view!.get('#policy-document-0-content').element as HTMLTextAreaElement).value).toBe('# Unsaved policy\n\nFull ordinary body.')
  expect((view!.get('#legal-item-0-value').element as HTMLInputElement).value).toBe('Unsubmitted legal value')
  expect(captured).not.toContain('MUST-NOT-CAPTURE')
  expect(updateSystemConfig).not.toHaveBeenCalled(); expect(updateSiteSettings).not.toHaveBeenCalled(); expect(testSMTP).not.toHaveBeenCalled()
})
it('shows old policy/legal drafts after fresh revision conflicts and blocks saves across a second recovery', async () => {
  await open(); await view!.get('#policy-document-0-content').setValue('Old policy edit'); await view!.get('#legal-item-0-value').setValue('Old legal edit')
  vi.mocked(fetchSystemConfigs).mockResolvedValue(rows().map(row => ({ ...row, revision: 2 })) as any)
  await roundTrip(); await roundTrip()
  expect((view!.get('#policy-document-0-content').element as HTMLTextAreaElement).value).toBe('Old policy edit')
  expect((view!.get('#legal-item-0-value').element as HTMLInputElement).value).toBe('Old legal edit')
  const policySave = view!.findAll('button').find(button => button.text() === '保存政策文档')!
  const legalSave = view!.findAll('button').find(button => button.text() === '保存法律信息')!
  expect((policySave.element as HTMLButtonElement).disabled).toBe(true); expect((legalSave.element as HTMLButtonElement).disabled).toBe(true)
  const vm = view!.vm as any
  await vm.saveConfig(vm.configs.find((row: any) => row.config_key === 'site_policy_documents'))
  await vm.saveConfig(vm.configs.find((row: any) => row.config_key === 'site_legal_items'))
  expect(updateSystemConfig).not.toHaveBeenCalled()
})
it('keeps original config revision and performs only an explicit policy save', async () => {
  await open(); await view!.get('#policy-document-0-content').setValue('Explicit policy body')
  await roundTrip()
  expect(updateSystemConfig).not.toHaveBeenCalled()
  vi.mocked(updateSystemConfig).mockResolvedValue(config('site_policy_documents', [{ ...policy[0], content: 'Explicit policy body' }], 2) as any)
  await view!.findAll('button').find(button => button.text() === '保存政策文档')!.trigger('click'); await flushPromises()
  expect(updateSystemConfig).toHaveBeenCalledWith('site_policy_documents', [{ ...policy[0], content: 'Explicit policy body' }], 1)
})
it('keeps the current mounted body and refuses capture when recovery storage is too small', async () => {
  await open()
  const body = 'Ordinary body '.repeat(6000)
  await view!.get('#policy-document-0-content').setValue(body)
  const original = (view!.vm as any).drafts.site_policy_documents
  const result = captureReauthenticationDrafts(1, path)
  expect(result.ok).toBe(false); expect(result.message).toContain('草稿过大')
  expect((view!.vm as any).drafts.site_policy_documents).toBe(original)
  expect((view!.get('#policy-document-0-content').element as HTMLTextAreaElement).value).toBe(body)
  expect(updateSystemConfig).not.toHaveBeenCalled()
})
it('refuses malformed structure without truncating or replacing the mounted draft', async () => {
  await open()
  const vm = view!.vm as any; vm.drafts.site_policy_documents = '[invalid JSON'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  expect(vm.drafts.site_policy_documents).toBe('[invalid JSON')
  expect(updateSystemConfig).not.toHaveBeenCalled()
})
it('holds a draft when fresh metadata removes public editability', async () => {
  await open(); await view!.get('#policy-document-0-content').setValue('Public old draft')
  vi.mocked(fetchSystemConfigs).mockResolvedValue(rows().map(row => row.config_key === 'site_policy_documents' ? { ...row, is_public: false } : row) as any)
  await roundTrip()
  const vm = view!.vm as any
  expect(vm.drafts.site_policy_documents).toContain('Public old draft')
  await vm.saveConfig(vm.configs.find((row: any) => row.config_key === 'site_policy_documents'))
  expect(updateSystemConfig).not.toHaveBeenCalled()
})

it('restores incomplete legal fields as a draft rather than validating or saving them during recovery', async () => {
  await open(); await view!.get('#legal-item-0-label').setValue(''); await view!.get('#legal-item-0-url').setValue('/not-yet-valid')
  await roundTrip()
  expect((view!.get('#legal-item-0-label').element as HTMLInputElement).value).toBe('')
  expect((view!.get('#legal-item-0-url').element as HTMLInputElement).value).toBe('/not-yet-valid')
  expect(updateSystemConfig).not.toHaveBeenCalled()
})
