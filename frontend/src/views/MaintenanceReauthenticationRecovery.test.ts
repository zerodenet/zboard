import { defineComponent, h, reactive } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Maintenance from './Maintenance.vue'
import FormField from '../components/FormField.vue'
import { fetchDatabaseMigrationStatus, fetchSystemConfigs, preflightDatabaseMigration, startDatabaseMigration, updateMaintenanceSettings } from '../api/client'
import { fetchNavigation } from '../api/menus'
import { confirmAction } from '../utils/feedback'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity, REAUTH_DRAFT_MANIFEST } from '../utils/reauthenticationDrafts'

const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn() }))
vi.mock('../api/client', () => ({ fetchDatabaseMigrationStatus: vi.fn(), fetchSystemConfigs: vi.fn(), preflightDatabaseMigration: vi.fn(), startDatabaseMigration: vi.fn(), updateMaintenanceSettings: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true) }))
const path = '/admin/maintenance'
const draftKey = 'zboard.reauth-draft.v1.admin.maintenance.text'
const status = (enabled = false) => ({ source_driver: 'mysql', maintenance: { enabled, title: 'Server title', message: 'Server message', migration_in_progress: false, migration_cutover_pending: false } })
const configs = (revision = 3, title = 'Server title', enabled = false) => ['maintenance_title', 'maintenance_message', 'maintenance_enabled'].map((config_key, index) => ({ id: index + 1, config_key, name: config_key, value: config_key === 'maintenance_title' ? title : config_key === 'maintenance_message' ? 'Server message' : enabled, revision: revision + index, value_type: config_key === 'maintenance_enabled' ? 'bool' : 'string', is_secret: false, is_public: true, configured: true, description: '', updated_at: '' }))
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const toggle = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { type: 'checkbox', checked: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked) }) } })
let view: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks(); clearReauthenticationDrafts(); sessionStorage.clear()
  state.app = reactive({ token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0, loadSystemStatus: vi.fn(async () => undefined) })
  vi.mocked(fetchNavigation).mockResolvedValue({ page_available: true, revision: 1, nodes: [] } as any)
  vi.mocked(fetchDatabaseMigrationStatus).mockResolvedValue(status() as any)
  vi.mocked(fetchSystemConfigs).mockResolvedValue(configs() as any)
  vi.mocked(updateMaintenanceSettings).mockResolvedValue(status().maintenance)
  vi.mocked(confirmAction).mockResolvedValue(true)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts(); sessionStorage.clear(); vi.restoreAllMocks() })
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component: Maintenance }] })
  await router.push(path); await router.isReady()
  view = shallowMount(Maintenance, { global: { plugins: [router], renderStubDefaultSlot: true, components: { UiInput: input, UiTextarea: input, UiCheckbox: toggle, UiButton: defineComponent({ template: '<button><slot /></button>' }), FormField }, stubs: { UiInput: false, UiTextarea: false, UiCheckbox: false, UiButton: false, FormField: false, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, UiSection: { template: '<section><slot /><slot name="meta" /></section>' }, SettingsPageLayout: { template: '<main><slot /></main>' }, PageAlert: { template: '<aside><slot /><slot name="actions" /></aside>' }, DatabaseMigrationDatasource: true } } })
  await flushPromises()
}
function current() { return view!.vm as any }
function noWrites() { expect(updateMaintenanceSettings).not.toHaveBeenCalled(); expect(preflightDatabaseMigration).not.toHaveBeenCalled(); expect(startDatabaseMigration).not.toHaveBeenCalled() }
function allStored() { return Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('') }
function capture() {
  const result = captureReauthenticationDrafts(1, path)
  expect(result).toMatchObject({ ok: true, count: 1 })
  return allStored()
}
async function roundTrip() {
  const stored = capture()
  view!.unmount(); view = undefined
  confirmReauthenticationDraftIdentity(1, true)
  await open()
  return stored
}
it('restores ordinary title/message but not the edited switch, preflight or migration confirmation; never submits', async () => {
  await open()
  current().maintenance.title = 'Operator title'; current().maintenance.message = 'Unsubmitted\nmaintenance notice'
  current().maintenance.enabled = true; current().confirmed = true; current().preflight = { target: 'DO-NOT-CAPTURE-PREFLIGHT', tables: 99 }
  const stored = await roundTrip()
  expect(current().maintenance).toMatchObject({ title: 'Operator title', message: 'Unsubmitted\nmaintenance notice', enabled: false })
  expect(current().confirmed).toBe(false); expect(current().preflight).toBeNull(); expect(current().targetDatasource).toBe('')
  expect(stored).not.toContain('DO-NOT-CAPTURE-PREFLIGHT'); expect(stored).not.toContain('confirmed'); expect(stored).not.toContain('target_datasource')
  noWrites()
})
it('captures all three original CAS boundaries and saves only after a fresh explicit action', async () => {
  await open(); current().maintenance.title = 'Recovered title'
  capture()
  const data = JSON.parse(sessionStorage.getItem(draftKey)!).data
  expect(Object.keys(data).sort()).toEqual(['baseline', 'message', 'title'])
  expect(data.baseline.maintenance_enabled).toEqual({ value: false, revision: 5 })
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true); await open(); noWrites()
  await current().saveMaintenance()
  expect(updateMaintenanceSettings).toHaveBeenCalledWith(expect.objectContaining({ title: 'Recovered title', enabled: false, expected_revisions: { maintenance_title: 3, maintenance_message: 4, maintenance_enabled: 5 } }))
  expect(preflightDatabaseMigration).not.toHaveBeenCalled(); expect(startDatabaseMigration).not.toHaveBeenCalled()
})
it('refuses full-page capture after any DSN edit, writes no secret and leaves the mounted editor intact', async () => {
  await open(); current().maintenance.title = 'Keep mounted'; current().targetDatasource = 'user:DO-NOT-STORE-DSN@tcp(example)/db'
  const write = vi.spyOn(Storage.prototype, 'setItem')
  expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: false, count: 0 })
  expect(current().maintenance.title).toBe('Keep mounted'); expect(current().targetDatasource).toContain('DO-NOT-STORE-DSN')
  expect(allStored()).not.toContain('DO-NOT-STORE-DSN')
  expect(write.mock.calls.flat().join('')).not.toContain('DO-NOT-STORE-DSN')
  current().targetDatasource = ''
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  write.mockRestore(); noWrites()
})
it.each(['revision', 'value', 'enabled', 'missing', 'secret'] as const)('retains the original baseline and blocks writes after fresh %s drift, including recapture', async kind => {
  await open(); current().maintenance.message = 'Old editor message'
  const fresh = configs()
  if (kind === 'revision') fresh[0]!.revision++
  if (kind === 'value') fresh[0]!.value = 'Changed without revision'
  if (kind === 'enabled') fresh[2]!.value = true
  if (kind === 'missing') fresh.pop()
  if (kind === 'secret') fresh[0]!.is_secret = true
  vi.mocked(fetchSystemConfigs).mockResolvedValue(fresh as any)
  await roundTrip()
  expect(current().maintenance.message).toBe('Old editor message'); expect(current().recoveryConflict).toBe(true)
  capture()
  expect(JSON.parse(sessionStorage.getItem(draftKey)!).data.baseline.maintenance_title).toEqual({ value: 'Server title', revision: 3 })
  await current().saveMaintenance(); noWrites()
})
it('rechecks current revisions immediately before an explicit save without upgrading the old draft', async () => {
  await open(); current().maintenance.title = 'Old title edit'; await roundTrip()
  vi.mocked(fetchSystemConfigs).mockResolvedValue(configs(4) as any)
  await current().saveMaintenance()
  expect(current().recoveryConflict).toBe(true); expect(current().maintenance.title).toBe('Old title edit'); noWrites()
})
it('keeps ordinary text and its original baseline during refresh or migration polling', async () => {
  await open(); current().maintenance.title = 'Still typing'
  vi.mocked(fetchSystemConfigs).mockResolvedValue(configs(4, 'Changed server') as any)
  await current().load()
  expect(current().maintenance.title).toBe('Still typing'); expect(current().recoveryConflict).toBe(true)
  capture()
  expect(JSON.parse(sessionStorage.getItem(draftKey)!).data.baseline.maintenance_title.revision).toBe(3)
  noWrites()
})
it('allows explicit reload to discard a conflicting ordinary draft without executing anything', async () => {
  await open(); current().maintenance.title = 'Old draft'
  vi.mocked(fetchSystemConfigs).mockResolvedValue(configs(4, 'Changed server') as any)
  vi.mocked(fetchDatabaseMigrationStatus).mockResolvedValue({ ...status(), maintenance: { ...status().maintenance, title: 'Changed server' } } as any)
  await roundTrip(); await current().reload(); await flushPromises()
  expect(confirmAction).toHaveBeenCalled(); expect(current().maintenance.title).toBe('Changed server'); expect(current().recoveryConflict).toBe(false); noWrites()
})
it.each(['actor', 'permission'] as const)('does not reveal an old draft after fresh %s changes', async kind => {
  await open(); current().maintenance.title = 'Prior account text'; capture(); view!.unmount(); view = undefined
  if (kind === 'actor') state.app.user.id = 2
  else vi.mocked(fetchNavigation).mockResolvedValue({ page_available: false, revision: 2, nodes: [] } as any)
  confirmReauthenticationDraftIdentity(state.app.user.id, true); await open()
  expect(current().maintenance.title).toBe('Server title'); expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull(); noWrites()
})
it('retains the stored draft when fresh resource loading fails and restores only after an explicit retry', async () => {
  await open(); current().maintenance.title = 'Retryable text'; capture(); view!.unmount(); view = undefined
  vi.mocked(fetchSystemConfigs).mockRejectedValueOnce(new Error('offline'))
  confirmReauthenticationDraftIdentity(1, true); await open()
  expect(current().maintenance.title).not.toBe('Retryable text'); expect(sessionStorage.getItem(draftKey)).not.toBeNull()
  await current().retryDraftRecovery(); await flushPromises()
  expect(current().maintenance.title).toBe('Retryable text'); expect(sessionStorage.getItem(draftKey)).toBeNull(); noWrites()
})
it('ignores a late fresh response after the actor/session changed', async () => {
  await open(); current().maintenance.title = 'Prior text'; capture(); view!.unmount(); view = undefined
  let resolve!: (value: any) => void
  vi.mocked(fetchSystemConfigs).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  confirmReauthenticationDraftIdentity(1, true); await open()
  state.app.user.id = 2; state.app.sessionEpoch++
  resolve(configs()); await flushPromises()
  expect(current().maintenance.title).not.toBe('Prior text'); noWrites()
})

it('clears an inaccessible draft on fresh resource permission denial without executing a write', async () => {
  await open(); current().maintenance.title = 'Restricted text'; capture(); view!.unmount(); view = undefined
  vi.mocked(fetchSystemConfigs).mockRejectedValueOnce({ response: { status: 403 } })
  confirmReauthenticationDraftIdentity(1, true); await open()
  expect(current().maintenance.title).not.toBe('Restricted text'); expect(sessionStorage.getItem(draftKey)).toBeNull(); noWrites()
})
it('does not overwrite newer local text when the extra recovery fresh read finishes late', async () => {
  await open(); current().maintenance.title = 'Recovered old text'; capture(); view!.unmount(); view = undefined
  let resolve!: (value: any) => void
  vi.mocked(fetchSystemConfigs).mockResolvedValueOnce(configs() as any).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  confirmReauthenticationDraftIdentity(1, true); await open()
  current().maintenance.title = 'New local work'
  resolve(configs()); await flushPromises()
  expect(current().maintenance.title).toBe('New local work'); noWrites()
})
it('ignores fresh-save responses from an old identity before making the write', async () => {
  await open(); current().maintenance.title = 'Prior actor edit'
  let resolve!: (value: any) => void
  vi.mocked(fetchSystemConfigs).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const pending = current().saveMaintenance()
  state.app.user.id = 2; state.app.sessionEpoch++
  resolve(configs()); await pending; await flushPromises(); noWrites()
})

it('releases the connection capture barrier only after explicitly discarding that local draft, keeping ordinary text', async () => {
  await open(); current().maintenance.title = 'Keep ordinary text'; current().targetDatasource = 'DO-NOT-STORE-DSN'; current().confirmed = true; current().preflight = { target: 'old', tables: 1 }
  vi.mocked(confirmAction).mockResolvedValueOnce(false)
  await current().discardConnectionDraft()
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  await current().discardConnectionDraft()
  expect(current().targetDatasource).toBe(''); expect(current().preflight).toBeNull(); expect(current().confirmed).toBe(false)
  expect(current().maintenance.title).toBe('Keep ordinary text')
  const stored = capture(); expect(stored).not.toContain('DO-NOT-STORE-DSN'); noWrites()
})
