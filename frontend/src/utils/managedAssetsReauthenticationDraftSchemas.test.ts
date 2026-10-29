import { expect, it } from 'vitest'
import { certificateCreateRecoveryDraft, certificateDraftBaseline, certificateEditRecoveryDraft, certificateRenewalRecoveryDraft, dnsCreateRecoveryDraft, dnsEditRecoveryDraft, providerDraftBaseline } from './managedAssetsReauthenticationDraftSchemas'
const provider = { id: 1, revision: 7, provider_key: 'cloudflare' }
const dnsFields = { provider_account_id: 1, node_id: 2, domain_name: 'edge.example.test', ipv4_value: '1.1.1.1', ipv6_value: '', ttl: 60, proxied: false }
const editDNSFields = { provider_account_id: 1, node_id: 2, domain_name: 'edge.example.test', record_type: 'A', record_value: '1.1.1.1', ttl: 60, proxied: false }
const certificate = { node_id: 2, provider_account_id: 1, name: 'Edge', domains: ['edge.example.test'], contact_email: 'operator@example.test', challenge_type: 'dns-01', webroot_path: '', environment: 'production', auto_renew: false, renew_before_days: 30 }
it('serializes only ordinary DNS fields and drops takeover approval and all structured credentials', () => {
  const safe = dnsCreateRecoveryDraft({ fields: { ...dnsFields, takeover_existing: true, api_token: 'DNS-SECRET', ssh_password: 'SSH-SECRET' }, provider: { ...provider, credential_prefix: 'PREFIX', api_token: 'TOKEN' }, operatingRecord: 42, saving: true })
  expect(safe).toEqual({ fields: dnsFields, provider })
  expect(JSON.stringify(safe)).not.toMatch(/SECRET|PREFIX|TOKEN|takeover|operating|saving/)
  expect(providerDraftBaseline({ ...provider, private_key: 'KEY' })).toEqual(provider)
})
it('keeps original DNS revision and ordinary baseline instead of adopting a new server version', () => {
  const value = { id: 5, revision: 4, fields: { ...editDNSFields, record_value: '8.8.8.8' }, baseline: editDNSFields, provider }
  expect(dnsEditRecoveryDraft(value)).toEqual(value)
  expect(dnsEditRecoveryDraft({ ...value, fields: { ...value.fields, domain_name: 'another.example.test' } })).toBeUndefined()
  expect(dnsEditRecoveryDraft({ ...value, revision: -1 })).toBeUndefined()
})
it('does not serialize ACME, private keys, operation state or deploy approval in certificate creates', () => {
  const fields = { ...certificate, domains: 'edge.example.test', challenge_type: 'dns-01', api_token: 'TOKEN', private_key: 'PRIVATE', acme_credentials: 'ACME', issue_approved: true }
  const safe = certificateCreateRecoveryDraft({ fields, provider, latest_operation: { status: 'running' }, issue_inflight: true })
  expect(safe?.fields).toEqual({ ...certificate, domains: 'edge.example.test' })
  expect(JSON.stringify(safe)).not.toMatch(/TOKEN|PRIVATE|ACME|approved|latest_operation|inflight/)
})
it('limits certificate baselines to editable and immutable public identity fields', () => {
  const baseline = certificateDraftBaseline({ ...certificate, private_key: 'PRIVATE', key_path: '/private/key', latest_operation: { status: 'running' } })!
  const fields = { ...baseline.fields, name: 'Draft name', private_key: 'PRIVATE' }
  const safe = certificateEditRecoveryDraft({ id: 9, revision: 6, baseline, fields })!
  expect(safe.revision).toBe(6)
  expect(safe.baseline).toEqual(baseline)
  expect(safe.fields.name).toBe('Draft name')
  expect(JSON.stringify(safe)).not.toMatch(/PRIVATE|key_path|latest_operation/)
  const policy = certificateRenewalRecoveryDraft({ id: 9, revision: 6, baseline, fields: { auto_renew: false, renew_before_days: 10, approved: true } })!
  expect(policy.fields).toEqual({ auto_renew: false, renew_before_days: 10 })
})
it('rejects malformed, oversized and cross-provider draft identities without storing arbitrary payloads', () => {
  expect(dnsCreateRecoveryDraft({ fields: dnsFields, provider: { ...provider, id: 22 } })).toBeUndefined()
  expect(dnsCreateRecoveryDraft({ fields: { ...dnsFields, domain_name: '界'.repeat(400) }, provider })).toBeUndefined()
  expect(certificateCreateRecoveryDraft({ fields: { ...certificate, domains: 'x'.repeat(8193) }, provider })).toBeUndefined()
  expect(certificateRenewalRecoveryDraft({ id: 0, revision: 1, baseline: certificateDraftBaseline(certificate), fields: { auto_renew: false, renew_before_days: 30 } })).toBeUndefined()
})
