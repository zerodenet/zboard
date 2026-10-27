import { expect, it } from 'vitest'
import { nodeAssetBaseline, nodeAssetRecovery } from './nodeAssetRecovery'
import { nodeGroupBaseline, nodeGroupRecovery } from './nodeGroupRecovery'
import { safeReauthenticationRoute } from './reauthenticationDrafts'
const fields = { name: 'edge', region: 'HK', address: 'edge.example.test', remark: 'ordinary note', lifecycle_status: 'active', is_enabled: true }
it('allowlists ordinary assets and never serializes credentials or execution intents', () => {
  const draft = nodeAssetRecovery({ entityID: 1, baseline: nodeAssetBaseline(fields), fields: { ...fields, ssh_password: 'secret-password', ssh_private_key: 'secret-key', ssh_privilege_password: 'secret-sudo', secretModal: 'secret-token', pendingBBRNodeID: 1, enable_bbr: true }, task_id: 9 })!
  expect(draft.fields).toEqual(fields)
  expect(JSON.stringify(draft)).not.toMatch(/secret-|ssh_|pendingBBR|enable_bbr|task_id/)
  expect(nodeAssetRecovery({ ...draft, entityID: 1, baseline: '' })).toBeUndefined()
  expect(nodeAssetRecovery({ ...draft, fields: { ...fields, remark: 'x'.repeat(20001) } })).toBeUndefined()
})
it('compares canonical original memberships without ordering or runtime observation changes', () => {
  const group = { name: 'group', code: 'group', description: '', is_enabled: true, protocol_endpoint_ids: [3, 1], network_entry_ids: [7] }
  const baseline = nodeGroupBaseline(group)
  expect(nodeGroupBaseline({ ...group, protocol_endpoint_ids: [1, 3], observed_at: 'later' })).toBe(baseline)
  expect(nodeGroupBaseline({ ...group, network_entry_ids: [8] })).not.toBe(baseline)
  expect(nodeAssetBaseline({ ...fields, bytes_up: 1234, updated_at: 'later' })).toBe(nodeAssetBaseline(fields))
  const draft = nodeGroupRecovery({ entityID: 1, revision: 3, baseline, originalEndpointIDs: [3, 1], originalEntryIDs: [7], fields: { ...group, token: 'excluded' } })!
  expect(draft.revision).toBe(3); expect(draft.originalEndpointIDs).toEqual([3, 1])
  expect(JSON.stringify(draft)).not.toContain('excluded')
  expect(nodeGroupRecovery({ ...draft, originalEndpointIDs: [1, 1] })).toBeUndefined()
  expect(nodeGroupRecovery({ ...draft, fields: { ...group, network_entry_ids: [-1] } })).toBeUndefined()
})
it('preserves only view-specific ordinary node and group query position', () => {
  expect(safeReauthenticationRoute('/admin/nodes?q=HK&lifecycle=maintenance&connector=offline&sort=region&direction=asc&density=comfortable&page=2&limit=25&node=8&tab=protocols&protocol_page=3&protocol_limit=50&retirement=8&ssh_password=secret&token=x&task=9'))
    .toBe('/admin/nodes?q=HK&lifecycle=maintenance&connector=offline&sort=region&direction=asc&density=comfortable&page=2&limit=25&node=8&tab=protocols&protocol_page=3&protocol_limit=50')
  expect(safeReauthenticationRoute('/admin/node-groups?q=HK&enabled=false&page=3&group=7&secret=x')).toBe('/admin/node-groups?q=HK&enabled=false&page=3&group=7')
  expect(safeReauthenticationRoute('/account/tickets?connector=offline&enabled=true&sort=region')).toBe('/account/tickets')
  expect(safeReauthenticationRoute('/admin/nodes?sort=token&connector=secret&protocol_page=-1&protocol_limit=999&density=comfortable&density=secret')).toBe('/admin/nodes')
})
