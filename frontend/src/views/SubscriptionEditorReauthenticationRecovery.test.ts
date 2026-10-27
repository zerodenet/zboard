import { defineComponent, h } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import SubscriptionTemplates from './SubscriptionTemplates.vue'
import SubscriptionRuleSets from './SubscriptionRuleSets.vue'
import FormField from '../components/FormField.vue'
import { fetchSubscriptionTemplate, fetchSubscriptionTemplatesPage, createSubscriptionTemplate, updateSubscriptionTemplate, previewSubscriptionTemplate } from '../api/client'
import { fetchManagedRuleSet, fetchManagedRuleSetContent, fetchManagedRuleSetsPage, createManagedRuleSet, updateManagedRuleSet, importManagedRuleSet } from '../api/managedRuleSets'
import { fetchNavigation } from '../api/menus'
import { defaultSubscriptionCustomization } from '../utils/subscriptionTemplateEditor'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn() }))
vi.mock('../api/client', () => ({ fetchSubscriptionTemplate: vi.fn(), fetchSubscriptionTemplatesPage: vi.fn(), createSubscriptionTemplate: vi.fn(), updateSubscriptionTemplate: vi.fn(), previewSubscriptionTemplate: vi.fn(), deleteSubscriptionTemplate: vi.fn(), fetchSubscriptionRuleSetsPage: vi.fn() }))
vi.mock('../api/managedRuleSets', () => ({ fetchManagedRuleSet: vi.fn(), fetchManagedRuleSetContent: vi.fn(), fetchManagedRuleSetsPage: vi.fn(), createManagedRuleSet: vi.fn(), updateManagedRuleSet: vi.fn(), importManagedRuleSet: vi.fn(), deleteManagedRuleSet: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn(async () => true) }))
const template = () => ({ id: 1, revision: 3, name: 'Server template', slug: 'server-template', description: '', renderer: 'clash', customization: defaultSubscriptionCustomization('clash'), is_active: true, sort_order: 0, updated_at: '', created_at: '' })
const rule = () => ({ id: 2, revision: 4, name: 'Server rules', description: '', tag: 'ads', interval: 3600, is_active: true, source_url: '', source_format: 'auto', usage_count: 1, updated_at: '', rule_count: 1, content_bytes: 40 })
const content = () => ({ id: 2, revision: 4, content: '{"version":1,"rules":[]}', rule_count: 0, content_bytes: 24, content_sha256: 'server' })
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const button = defineComponent({ template: '<button><slot /></button>' })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined, captured = ''
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts(); captured = ''
  state.app = { token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 1, nodes: [], page_available: true })
  vi.mocked(fetchSubscriptionTemplate).mockImplementation(async () => template() as any)
  vi.mocked(fetchSubscriptionTemplatesPage).mockImplementation(async () => ({ items: [template()], total: 1 }) as any)
  vi.mocked(fetchManagedRuleSet).mockImplementation(async () => rule() as any)
  vi.mocked(fetchManagedRuleSetContent).mockImplementation(async () => content() as any)
  vi.mocked(fetchManagedRuleSetsPage).mockImplementation(async () => ({ items: [rule()], total: 1 }) as any)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open(kind: 'template' | 'rule', path?: string) {
  const base = kind === 'template' ? '/admin/subscription-templates' : '/admin/subscription-templates/rule-sets'
  const component = kind === 'template' ? SubscriptionTemplates : SubscriptionRuleSets
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: base, component }] })
  await router.push(path || base)
  view = shallowMount(component as any, { global: { plugins: [router], renderStubDefaultSlot: true, components: { UiInput: input, UiTextarea: input, UiButton: button }, stubs: { UiInput: false, UiTextarea: false, UiButton: false, FormField, ModalDialog: modal, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { props: ['title'], template: '<aside><strong v-if="title">{{ title }}</strong><slot /><slot name="actions" /></aside>' } } } })
  await flushPromises()
  return path || base
}
async function roundTrip(kind: 'template' | 'rule', path: string, actor = 1, admin = true) {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  captured = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('')
  view!.unmount(); view = undefined; state.app.user.id = actor; state.app.isAdmin = admin
  confirmReauthenticationDraftIdentity(actor, admin)
  await open(kind, path)
}
function noWrites() {
  expect(createSubscriptionTemplate).not.toHaveBeenCalled(); expect(updateSubscriptionTemplate).not.toHaveBeenCalled(); expect(previewSubscriptionTemplate).not.toHaveBeenCalled()
  expect(createManagedRuleSet).not.toHaveBeenCalled(); expect(updateManagedRuleSet).not.toHaveBeenCalled(); expect(importManagedRuleSet).not.toHaveBeenCalled()
}
it('restores template ordinary and structured fields with original revision and no automatic preview or application', async () => {
  const path = await open('template', '/admin/subscription-templates?template=1'), vm = view!.vm as any
  vm.form.name = 'Local title'; vm.form.customization.policy_groups[0].name = 'Local group'; vm.form.customization.dns.enabled = true
  await roundTrip('template', path)
  const restored = view!.vm as any
  expect(restored.form.name).toBe('Local title'); expect(restored.form.revision).toBe(3); expect(restored.form.customization.policy_groups[0].name).toBe('Local group'); noWrites()
  await restored.save()
  expect(updateSubscriptionTemplate).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Local title', expected_revision: 3 }))
})
it('preserves all per-renderer ordinary draft caches without importing preview/approval state', async () => {
  const path = await open('template', '/admin/subscription-templates?template=1'), vm = view!.vm as any
  vm.form.customization.policy_groups[0].name = 'Clash edit'; vm.form.renderer = 'sing-box'; vm.form.customization.policy_groups[0].name = 'Sing edit'
  await roundTrip('template', path)
  const restored = view!.vm as any
  expect(restored.form.renderer).toBe('sing-box'); expect(restored.form.customization.policy_groups[0].name).toBe('Sing edit')
  restored.form.renderer = 'clash'; await flushPromises()
  expect(restored.form.customization.policy_groups[0].name).toBe('Clash edit'); noWrites()
})
it('retains conflicting template drafts through recapture and blocks even programmatic save', async () => {
  const path = await open('template', '/admin/subscription-templates?template=1'), vm = view!.vm as any
  vm.form.name = 'Old local edit'
  vi.mocked(fetchSubscriptionTemplate).mockImplementation(async () => ({ ...template(), revision: 4, name: 'Other administrator' }) as any)
  await roundTrip('template', path); await roundTrip('template', path)
  const restored = view!.vm as any
  expect(restored.form.name).toBe('Old local edit'); expect(restored.form.revision).toBe(3); expect(restored.revisionConflict).toBe(true)
  await restored.save(); noWrites()
})
it('restores a new template without creating or applying it', async () => {
  const path = await open('template', '/admin/subscription-templates?template=new'), vm = view!.vm as any
  vm.form.name = 'New local'; vm.form.slug = 'new-local'
  await roundTrip('template', path)
  expect((view!.vm as any).form).toMatchObject({ id: 0, revision: 0, name: 'New local', slug: 'new-local' }); noWrites()
})
it.each(['raw', 'advanced', 'unknown', 'changed-url'] as const)('explicitly refuses complete template capture for unsafe %s content without clearing the editor', async (mode) => {
  const path = await open('template', '/admin/subscription-templates?template=1'), vm = view!.vm as any
  vm.form.name = 'Keep ordinary title'
  if (mode === 'raw') vm.rawCustomizationError = 'Invalid JSON in local child buffer'
  if (mode === 'advanced') vm.form.customization.advanced_source = 'password: DO-NOT-STORE'
  if (mode === 'unknown') vm.form.customization.policy_groups[0].private_key = 'DO-NOT-STORE'
  if (mode === 'changed-url') vm.form.customization.policy_groups[1].probe_url = 'https://user:DO-NOT-STORE@probe.test/path'
  await flushPromises()
  expect(view!.text()).toContain('不能暂存到登录页')
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  expect(vm.editorOpen).toBe(true); expect(vm.form.name).toBe('Keep ordinary title'); expect(sessionStorage.length).toBe(0); noWrites()
})
it('uses a freshly re-read unchanged private template URL without persisting its credential', async () => {
  const source = template(); source.customization.rule_sets = [{ tag: 'private', url: 'https://rules.test/path?token=DO-NOT-STORE', target: 'direct', interval: 3600, format: 'yaml', behavior: 'classical' }]
  vi.mocked(fetchSubscriptionTemplate).mockImplementation(async () => structuredClone(source) as any)
  const path = await open('template', '/admin/subscription-templates?template=1'); (view!.vm as any).form.description = 'Ordinary local description'
  await roundTrip('template', path); await roundTrip('template', path)
  expect(captured).not.toContain('DO-NOT-STORE')
  expect((view!.vm as any).form.customization.rule_sets[0].url).toBe(source.customization.rule_sets[0]!.url)
  expect((view!.vm as any).form.description).toBe('Ordinary local description'); noWrites()
})
it('retains a deleted template draft but cannot save it as if the original still exists', async () => {
  const path = await open('template', '/admin/subscription-templates?template=1'); (view!.vm as any).form.name = 'Local old title'
  vi.mocked(fetchSubscriptionTemplate).mockRejectedValue({ response: { status: 404 } })
  await roundTrip('template', path)
  expect((view!.vm as any).form.name).toBe('Local old title'); expect((view!.vm as any).revisionConflict).toBe(true)
  await (view!.vm as any).save(); noWrites()
})
it('restores manual rule text verbatim with original detail/content revision and no automatic import', async () => {
  const path = await open('rule'), vm = view!.vm as any; await vm.openEdit(rule())
  vm.form.name = 'Local rule name'; vm.form.content = '{"password":"literal ordinary rule body","rules":[]}'
  await roundTrip('rule', path)
  const restored = view!.vm as any
  expect(restored.form).toMatchObject({ id: 2, revision: 4, name: 'Local rule name', content: '{"password":"literal ordinary rule body","rules":[]}' }); noWrites()
  await restored.save()
  expect(updateManagedRuleSet).toHaveBeenCalledWith(2, expect.objectContaining({ expected_revision: 4, content: '{"password":"literal ordinary rule body","rules":[]}' }))
})
it('restores unchanged private remote sources from the server and preserves hidden manual body without serializing source credentials', async () => {
  const url = 'https://user:DO-NOT-STORE@rules.test/private/path?key=DO-NOT-STORE'
  vi.mocked(fetchManagedRuleSet).mockImplementation(async () => ({ ...rule(), source_url: url }) as any)
  const path = await open('rule'), vm = view!.vm as any; await vm.openEdit(rule()); vm.form.description = 'Local remote edit'; vm.form.content = 'Unsubmitted hidden ordinary body'
  await roundTrip('rule', path); await roundTrip('rule', path)
  expect(captured).not.toContain('DO-NOT-STORE')
  expect((view!.vm as any).form).toMatchObject({ source_url: url, mode: 'remote', content: 'Unsubmitted hidden ordinary body', description: 'Local remote edit', revision: 4 }); noWrites()
})
it('refuses an unsafe changed hidden source URL instead of losing it while recovering only manual content', async () => {
  const path = await open('rule'), vm = view!.vm as any; vm.openCreate(); vm.form.name = 'New local'; vm.form.source_url = 'https://rules.test/path?token=DO-NOT-STORE'; vm.form.mode = 'manual'
  await flushPromises()
  expect(view!.text()).toContain('不能暂存到登录页'); expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  expect(vm.form.source_url).toContain('DO-NOT-STORE'); expect(vm.editorOpen).toBe(true); expect(sessionStorage.length).toBe(0); noWrites()
})
it('keeps a new manual rule editor without implicit creation', async () => {
  const path = await open('rule'), vm = view!.vm as any; vm.openCreate(); vm.form.name = 'New rules'; vm.form.tag = 'new-rules'; vm.form.content = 'Plain unsubmitted body'
  await roundTrip('rule', path)
  expect((view!.vm as any).form).toMatchObject({ id: 0, revision: 0, name: 'New rules', content: 'Plain unsubmitted body' }); noWrites()
})
it.each(['changed', 'split', 'missing'] as const)('keeps original rule evidence but blocks submit when fresh detail/content is %s', async (kind) => {
  const path = await open('rule'), vm = view!.vm as any; await vm.openEdit(rule()); vm.form.content = 'Keep local body'
  if (kind === 'missing') { vi.mocked(fetchManagedRuleSet).mockRejectedValue({ response: { status: 404 } }); vi.mocked(fetchManagedRuleSetContent).mockRejectedValue({ response: { status: 404 } }) }
  else { vi.mocked(fetchManagedRuleSet).mockImplementation(async () => ({ ...rule(), revision: 5 }) as any); if (kind === 'changed') vi.mocked(fetchManagedRuleSetContent).mockImplementation(async () => ({ ...content(), revision: 5 }) as any) }
  await roundTrip('rule', path); await roundTrip('rule', path)
  expect((view!.vm as any).form.content).toBe('Keep local body'); expect((view!.vm as any).form.revision).toBe(4); expect((view!.vm as any).revisionConflict).toBe(true)
  await (view!.vm as any).save(); noWrites()
})
it.each(['template', 'rule'] as const)('leaves the stored %s draft pending when fresh detail reads fail', async (kind) => {
  const path = await open(kind, kind === 'template' ? '/admin/subscription-templates?template=1' : undefined), vm = view!.vm as any
  if (kind === 'rule') await vm.openEdit(rule())
  vm.form.name = 'Keep local title'
  const failure = { response: { status: 503 } }
  if (kind === 'template') vi.mocked(fetchSubscriptionTemplate).mockRejectedValue(failure)
  else vi.mocked(fetchManagedRuleSetContent).mockRejectedValue(failure)
  await roundTrip(kind, path)
  expect(sessionStorage.length).toBeGreaterThan(0); noWrites()
})
it.each(['template', 'rule'] as const)('requires fresh page permission before recovering %s content', async (kind) => {
  const path = await open(kind, kind === 'template' ? '/admin/subscription-templates?template=1' : undefined), vm = view!.vm as any
  if (kind === 'rule') await vm.openEdit(rule())
  vm.form.name = 'Hidden local title'
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 2, nodes: [], page_available: false })
  await roundTrip(kind, path)
  expect((view!.vm as any).form.name).not.toBe('Hidden local title'); expect(sessionStorage.length).toBe(0); noWrites()
})
it.each([{ actor: 2, admin: true }, { actor: 1, admin: false }])('does not restore former admin content after identity or role change %j', async ({ actor, admin }) => {
  const path = await open('rule'), vm = view!.vm as any; vm.openCreate(); vm.form.name = 'Private old draft'
  await roundTrip('rule', path, actor, admin)
  expect((view!.vm as any).editorOpen).toBe(false); expect(sessionStorage.length).toBe(0); noWrites()
})
it.each(['template', 'rule'] as const)('ignores a late fresh %s restore after the owning page is disposed', async (kind) => {
  const path = await open(kind, kind === 'template' ? '/admin/subscription-templates?template=1' : undefined), vm = view!.vm as any
  if (kind === 'rule') await vm.openEdit(rule())
  vm.form.name = 'Do not restore after disposal'
  let resolve!: (value: any) => void
  const pending = new Promise<any>(done => { resolve = done })
  if (kind === 'template') {
    let reads = 0
    vi.mocked(fetchSubscriptionTemplate).mockImplementation(() => ++reads === 1 ? Promise.resolve(template() as any) : pending)
  } else vi.mocked(fetchManagedRuleSetContent).mockImplementation(() => pending)
  await roundTrip(kind, path)
  const old = view!.vm as any
  const before = old.form.name
  view!.unmount(); view = undefined
  resolve(kind === 'template' ? template() : content()); await flushPromises()
  expect(old.form.name).toBe(before); noWrites()
})
