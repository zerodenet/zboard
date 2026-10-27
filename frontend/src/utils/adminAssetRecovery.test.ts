import { describe, expect, it } from 'vitest'
import { protocolAssetBaseline, protocolAssetRecovery, providerRecovery, userAccountBaseline, userAccountRecovery } from './adminAssetRecovery'
const at = '2026-10-27T00:00:00Z'
const fields = { node_id: 4, name: 'Draft', address: 'edge.test', listen_address: '0.0.0.0', port: 443, public_port: 443, node_group_memberships: [] }
describe('ordinary user, provider and protocol recovery schemas', () => {
  it('retains original identity and timestamp without passwords or permissions metadata', () => {
    const original = { id: 4, email: 'owner@example.test', status: 'active', is_admin: false, updated_at: at, password: 'SECRET' }
    const draft = userAccountRecovery({ id: 4, fields: { ...original, status: 'suspended' }, baseline: original, token: 'SECRET' })
    expect(draft).toEqual({ id: 4, fields: { email: original.email, status: 'suspended', is_admin: false }, baseline: userAccountBaseline(original) })
    expect(JSON.stringify(draft)).not.toContain('SECRET')
    expect(userAccountRecovery({ id: 5, fields: original, baseline: original })).toBeUndefined()
    expect(userAccountRecovery({ id: 4, fields: { ...original, email: 'other@example.test' }, baseline: original })).toBeUndefined()
    expect(userAccountBaseline({ ...original, updated_at: '' })).toBeUndefined()
  })
  it('keeps the server provider revision and never stores token or credential prefix', () => {
    const baseline = { id: 9, revision: 7, name: 'Old', provider_key: 'cloudflare', api_token: 'SECRET', credential_prefix: 'SECRET' }
    expect(providerRecovery({ id: 9, fields: { ...baseline, name: 'New' }, baseline })).toEqual({ id: 9, fields: { provider_key: 'cloudflare', name: 'New' }, baseline: { id: 9, revision: 7, provider_key: 'cloudflare', name: 'Old' } })
    expect(providerRecovery({ id: 9, fields: baseline, baseline: { ...baseline, revision: 0 } })).toBeUndefined()
  })
  it('retains ordinary endpoint identity and group versions while dropping plugin JSON and private keys', () => {
    const baseline = { ...fields, id: 2, protocol: 'vless', updated_at: at, config: 'SECRET', client_config: 'SECRET' }
    const draft = protocolAssetRecovery({ id: 2, baseline, fields: { ...fields, name: 'New', structured: { reality_private_key: 'SECRET' }, config: 'SECRET', egress_config: 'SECRET' } })
    expect(draft).toEqual({ id: 2, baseline: protocolAssetBaseline(baseline), fields: { ...fields, name: 'New' } })
    expect(JSON.stringify(draft)).not.toContain('SECRET')
    expect(protocolAssetRecovery({ id: 2, baseline: { ...baseline, updated_at: '' }, fields })).toBeUndefined()
    expect(protocolAssetRecovery({ id: 0, baseline: null, fields: { ...fields, node_group_memberships: [{ node_group_id: 1, revision: 3 }, { node_group_id: 1, revision: 3 }] } })).toBeUndefined()
  })
})
