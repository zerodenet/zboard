import { defineComponent, h } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Settings from './Settings.vue'
import RegistrationSettings from './RegistrationSettings.vue'
import EmailTemplateManager from '../components/EmailTemplateManager.vue'
import FormField from '../components/FormField.vue'
import { fetchSystemConfigs, updateSiteSettings, updateSystemConfig, fetchEmailTemplates, updateEmailTemplate, createEmailTemplate, previewEmailTemplate, testSMTP } from '../api/client'
import { fetchNavigation } from '../api/menus'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const state = vi.hoisted(() => ({ setup: { site_name: 'Server', site_url: 'https://panel.example', allow_registration: false }, app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn(async () => ({ page_available: true, revision: 1, nodes: [] })) }))
vi.mock('../api/client', () => ({ fetchSystemConfigs: vi.fn(), updateSiteSettings: vi.fn(), updateSystemConfig: vi.fn(), fetchEmailTemplates: vi.fn(), updateEmailTemplate: vi.fn(), createEmailTemplate: vi.fn(), previewEmailTemplate: vi.fn(), testSMTP: vi.fn(), deleteEmailTemplate: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn(async () => true) }))
const config = (key: string, value: unknown, revision = 1, secret = false) => ({ id: 1, config_key: key, name: key, value, revision, value_type: typeof value === 'boolean' ? 'bool' : 'string', is_public: key.startsWith('site_'), is_secret: secret, configured: secret, description: '', updated_at: '', input: { control: 'text' } })
const template = { id: 1, revision: 1, name: 'Server template', slug: 'server-template', subject_template: 'Subject', body_template: 'Server body', category: 'operational', is_active: true, sort_order: 0 }
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const toggle = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { type: 'checkbox', checked: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked) }) } })
const button = defineComponent({ template: '<button><slot /></button>' })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined
let captured = ''
beforeEach(() => {
  vi.clearAllMocks(); clearReauthenticationDrafts(); sessionStorage.clear(); captured = ''
  vi.mocked(fetchNavigation).mockResolvedValue({ page_available: true, revision: 1, nodes: [] } as any)
  state.setup = { site_name: 'Server', site_url: 'https://panel.example', allow_registration: false }
  state.app = { token: 'verified', user: { id: 1, email: 'admin@example.test' }, isAdmin: true, reauthRequired: false, sessionEpoch: 0, siteName: 'Server', installation: state.setup, loadSetupStatus: vi.fn(async () => state.setup), loadPublicConfigs: vi.fn() }
  vi.mocked(fetchSystemConfigs).mockResolvedValue([config('site_desc', 'Server description'), config('register_email_verification', false), config('smtp_password', '', 1, true)] as any)
  vi.mocked(fetchEmailTemplates).mockResolvedValue([structuredClone(template)] as any)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open(kind: 'site' | 'registration' | 'template') {
  const path = kind === 'registration' ? '/admin/settings/registration' : kind === 'template' ? '/admin/settings/email' : '/admin/settings/site'
  const component = kind === 'site' ? Settings : kind === 'registration' ? RegistrationSettings : EmailTemplateManager
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component, meta: { settingsSection: kind === 'site' ? 'site' : 'email' } }] })
  await router.push(path)
  view = shallowMount(component as any, { props: kind === 'template' ? { mode: 'operational' } : {}, global: { plugins: [router], renderStubDefaultSlot: true, components: { UiInput: input, UiTextarea: input, UiButton: button }, stubs: { UiInput: false, UiTextarea: false, UiButton: false, UiSwitch: toggle, FormField, ModalDialog: modal, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { template: '<aside><slot /></aside>' } } } })
  await flushPromises()
  return path
}
async function roundTrip(kind: 'site' | 'registration' | 'template', path: string) {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  captured = allStored()
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true)
  await open(kind)
}
function allStored() { return Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('') }
it('restores site text and selected public keys without serializing SMTP or submitting', async () => {
  const path = await open('site'), vm = view!.vm as any
  vm.form.site_name = 'Unsaved site'; vm.drafts.site_desc = 'Unsaved public description'; vm.drafts.smtp_password = 'DO-NOT-CAPTURE-SMTP'
  await roundTrip('site', path)
  expect((view!.get('#settings-site-name').element as HTMLInputElement).value).toBe('Unsaved site')
  expect((view!.vm as any).drafts.site_desc).toBe('Unsaved public description')
  expect(captured).not.toContain('DO-NOT-CAPTURE-SMTP')
  expect(captured).toContain('Unsaved public description')
  expect(updateSiteSettings).not.toHaveBeenCalled(); expect(updateSystemConfig).not.toHaveBeenCalled(); expect(testSMTP).not.toHaveBeenCalled()
})
it('retains conflicting site/config drafts and blocks programmatic saves through a second recovery', async () => {
  const path = await open('site'), vm = view!.vm as any
  vm.form.site_name = 'Old site edit'; vm.drafts.site_desc = 'Old text edit'
  state.setup = { ...state.setup, site_name: 'Changed elsewhere' }
  vi.mocked(fetchSystemConfigs).mockResolvedValue([config('site_desc', 'New server text', 2)] as any)
  await roundTrip('site', path); await roundTrip('site', path)
  const restored = view!.vm as any
  expect(restored.form.site_name).toBe('Old site edit'); expect(restored.drafts.site_desc).toBe('Old text edit')
  await restored.saveSiteSettings(); await restored.saveConfigGroup('content', '公开文案', ['site_desc'])
  expect(updateSiteSettings).not.toHaveBeenCalled(); expect(updateSystemConfig).not.toHaveBeenCalled()
})
it('restores both registration switches without saving or enabling mail automatically', async () => {
  const path = await open('registration'), vm = view!.vm as any
  vm.registrationEnabled = true; vm.verificationEnabled = true
  await roundTrip('registration', path)
  expect((view!.vm as any).registrationEnabled).toBe(true); expect((view!.vm as any).verificationEnabled).toBe(true)
  expect(updateSiteSettings).not.toHaveBeenCalled(); expect(updateSystemConfig).not.toHaveBeenCalled()
})
it('blocks conflicting registration and verification edits even after recapture', async () => {
  const path = await open('registration'), vm = view!.vm as any
  vm.registrationEnabled = true; vm.verificationEnabled = true
  state.setup = { ...state.setup, site_url: 'https://changed.example' }
  vi.mocked(fetchSystemConfigs).mockResolvedValue([config('register_email_verification', false, 2)] as any)
  await roundTrip('registration', path); await roundTrip('registration', path)
  await (view!.vm as any).saveRegistration(); await (view!.vm as any).saveVerification()
  expect(updateSiteSettings).not.toHaveBeenCalled(); expect(updateSystemConfig).not.toHaveBeenCalled()
})
it('restores a template editor with its original revision and saves only after explicit action', async () => {
  const path = await open('template'), vm = view!.vm as any
  vm.openEditor(structuredClone(template)); vm.form.body_template = 'Unsubmitted message'
  await roundTrip('template', path)
  expect((view!.vm as any).form.body_template).toBe('Unsubmitted message')
  expect(updateEmailTemplate).not.toHaveBeenCalled(); expect(previewEmailTemplate).not.toHaveBeenCalled()
  await (view!.vm as any).saveTemplate()
  expect(updateEmailTemplate).toHaveBeenCalledWith(1, expect.objectContaining({ body_template: 'Unsubmitted message', expected_revision: 1 }))
})
it('preserves the old template draft but cannot submit against a newer fresh revision', async () => {
  const path = await open('template'), vm = view!.vm as any
  vm.openEditor(structuredClone(template)); vm.form.subject_template = 'Old edit'
  vi.mocked(fetchEmailTemplates).mockResolvedValue([{ ...template, revision: 2 }] as any)
  await roundTrip('template', path); await roundTrip('template', path)
  expect((view!.vm as any).form.subject_template).toBe('Old edit')
  await (view!.vm as any).saveTemplate()
  expect(updateEmailTemplate).not.toHaveBeenCalled(); expect(createEmailTemplate).not.toHaveBeenCalled()
})
it('does not restore before fresh resources load successfully', async () => {
  const path = await open('template'), vm = view!.vm as any
  vm.openEditor(structuredClone(template)); vm.form.name = 'Local edit'
  vi.mocked(fetchEmailTemplates).mockRejectedValue(new Error('unavailable'))
  await roundTrip('template', path)
  expect((view!.vm as any).editorOpen).toBe(false)
  expect(updateEmailTemplate).not.toHaveBeenCalled()
})

it('keeps new operational template fields unsubmitted on recovery', async () => {
  const path = await open('template'), vm = view!.vm as any
  vm.openCreate(); vm.form.name = 'New draft'; vm.form.slug = 'new-draft'; vm.form.subject_template = 'New subject'; vm.form.body_template = 'New body'; vm.form.sort_order = 3
  await roundTrip('template', path)
  expect((view!.vm as any).form).toMatchObject({ id: 0, revision: 0, name: 'New draft', slug: 'new-draft', category: 'operational', body_template: 'New body', sort_order: 3 })
  expect(createEmailTemplate).not.toHaveBeenCalled(); expect(updateEmailTemplate).not.toHaveBeenCalled(); expect(previewEmailTemplate).not.toHaveBeenCalled()
})
it('does not open a recovered template after fresh page permission is denied', async () => {
  const path = await open('template'), vm = view!.vm as any
  vm.openEditor(structuredClone(template)); vm.form.name = 'Draft'
  vi.mocked(fetchNavigation).mockResolvedValue({ page_available: false, revision: 2, nodes: [] } as any)
  await roundTrip('template', path)
  expect((view!.vm as any).editorOpen).toBe(false)
  expect(view!.text()).toContain('当前页面权限已变化')
  expect(updateEmailTemplate).not.toHaveBeenCalled()
})

it('uses the original public-config revision only on an explicit save', async () => {
  const path = await open('site')
  ;(view!.vm as any).drafts.site_desc = 'Recovered description'
  await roundTrip('site', path)
  expect(updateSystemConfig).not.toHaveBeenCalled()
  vi.mocked(updateSystemConfig).mockResolvedValue(config('site_desc', 'Recovered description', 2) as any)
  await (view!.vm as any).saveConfigGroup('content', '公开文案', ['site_desc'])
  expect(updateSystemConfig).toHaveBeenCalledWith('site_desc', 'Recovered description', 1)
})
