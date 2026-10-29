import { expect, it } from 'vitest'
import { maintenanceConfigBaseline, maintenanceRecoveryDraft, sameMaintenanceBaseline } from './maintenanceReauthenticationDraft'

const original = {
  maintenance_title: { value: 'Server title', revision: 3 },
  maintenance_message: { value: 'Server message', revision: 4 },
  maintenance_enabled: { value: false, revision: 5 },
}
const draft = () => ({ title: '', message: 'Unsubmitted\ntext', baseline: structuredClone(original) })
it('keeps editable ordinary text and only the original three CAS boundaries', () => {
  expect(maintenanceRecoveryDraft(draft())).toEqual(draft())
  expect(maintenanceRecoveryDraft({ ...draft(), title: '未提交标题', message: '' })).toMatchObject({ title: '未提交标题', message: '' })
})
it.each(['enabled', 'target_datasource', 'targetDriver', 'preflight', 'confirmed', 'migration_in_progress'])('rejects execution/connection field %s instead of silently accepting a whole-page payload', key => {
  expect(maintenanceRecoveryDraft({ ...draft(), [key]: 'DO-NOT-STORE' })).toBeUndefined()
})
it('rejects a secret-bearing baseline, missing CAS, duplicate/injected keys and oversized text', () => {
  const changed = draft()
  expect(maintenanceRecoveryDraft({ ...changed, baseline: { ...original, dsn: 'DO-NOT-STORE' } })).toBeUndefined()
  expect(maintenanceRecoveryDraft({ ...changed, baseline: { ...original, maintenance_enabled: { value: true } } })).toBeUndefined()
  expect(maintenanceRecoveryDraft({ ...changed, baseline: { ...original, maintenance_title: { value: 'text', revision: 1, is_secret: false } } })).toBeUndefined()
  expect(maintenanceRecoveryDraft({ ...changed, title: 'x'.repeat(1025) })).toBeUndefined()
  expect(maintenanceRecoveryDraft({ blocked: true })).toBeUndefined()
})
it('requires all three original revisions, raw values and safe metadata when reading fresh configs', () => {
  const rows = Object.entries(original).map(([config_key, value]) => ({ config_key, ...value, value_type: config_key === 'maintenance_enabled' ? 'bool' : 'string', is_secret: false })) as any
  expect(maintenanceConfigBaseline(rows)).toEqual(original)
  expect(maintenanceConfigBaseline(rows.slice(1))).toBeUndefined()
  expect(maintenanceConfigBaseline([...rows, rows[0]])).toBeUndefined()
  expect(maintenanceConfigBaseline(rows.map((row: any) => ({ ...row, is_secret: true })))).toBeUndefined()
  expect(maintenanceConfigBaseline(rows.map((row: any) => row.config_key === 'maintenance_enabled' ? { ...row, value: 'false' } : row))).toBeUndefined()
  expect(sameMaintenanceBaseline(original, { ...original, maintenance_enabled: { value: false, revision: 6 } })).toBe(false)
  expect(sameMaintenanceBaseline(original, { ...original, maintenance_title: { value: 'Changed', revision: 3 } })).toBe(false)
})
