// Persist only ordinary editor fields. Never copy provider/node/certificate
// objects, credentials, paths to private keys, operation state or approvals.
const record = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown, max: number): value is string => typeof value === 'string' && new TextEncoder().encode(value).byteLength <= max
const id = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0
const integer = (value: unknown): value is number => Number.isSafeInteger(value)
export interface ProviderDraftBaseline { id: number; revision: number; provider_key: string }
export function providerDraftBaseline(value: unknown): ProviderDraftBaseline | undefined {
  if (!record(value) || !id(value.id) || value.id === 0 || !id(value.revision) || !text(value.provider_key, 160)) return
  return { id: value.id, revision: value.revision, provider_key: value.provider_key }
}
export function sameProviderDraftBaseline(before: ProviderDraftBaseline | null, fresh: unknown) {
  const after = providerDraftBaseline(fresh)
  return Boolean(before && after && before.id === after.id && before.revision === after.revision && before.provider_key === after.provider_key)
}
export interface DNSCreateDraftFields { provider_account_id: number; node_id: number; domain_name: string; ipv4_value: string; ipv6_value: string; ttl: number; proxied: boolean }
export function dnsCreateDraftFields(value: unknown): DNSCreateDraftFields | undefined {
  if (!record(value) || !id(value.provider_account_id) || !id(value.node_id) || !text(value.domain_name, 1024) || !text(value.ipv4_value, 160) || !text(value.ipv6_value, 160) || !integer(value.ttl) || typeof value.proxied !== 'boolean') return
  return { provider_account_id: value.provider_account_id, node_id: value.node_id, domain_name: value.domain_name, ipv4_value: value.ipv4_value, ipv6_value: value.ipv6_value, ttl: value.ttl, proxied: value.proxied }
}
export interface DNSEditDraftFields { provider_account_id: number; node_id: number; domain_name: string; record_type: 'A' | 'AAAA'; record_value: string; ttl: number; proxied: boolean }
export function dnsEditDraftFields(value: unknown): DNSEditDraftFields | undefined {
  if (!record(value) || !id(value.provider_account_id) || !id(value.node_id) || !text(value.domain_name, 1024) || !['A', 'AAAA'].includes(String(value.record_type)) || !text(value.record_value, 160) || !integer(value.ttl) || typeof value.proxied !== 'boolean') return
  return { provider_account_id: value.provider_account_id, node_id: value.node_id, domain_name: value.domain_name, record_type: value.record_type as 'A' | 'AAAA', record_value: value.record_value, ttl: value.ttl, proxied: value.proxied }
}
function providerBaseline(value: unknown): ProviderDraftBaseline | null | undefined { return value === null ? null : providerDraftBaseline(value) }
export interface DNSCreateRecoveryDraft { fields: DNSCreateDraftFields; provider: ProviderDraftBaseline | null }
export function dnsCreateRecoveryDraft(value: unknown): DNSCreateRecoveryDraft | undefined {
  if (!record(value)) return
  const fields = dnsCreateDraftFields(value.fields), provider = providerBaseline(value.provider)
  if (!fields || provider === undefined || provider && provider.id !== fields.provider_account_id) return
  return { fields, provider }
}
export interface DNSEditRecoveryDraft { id: number; revision: number; baseline: DNSEditDraftFields; fields: DNSEditDraftFields; provider: ProviderDraftBaseline | null }
export function dnsEditRecoveryDraft(value: unknown): DNSEditRecoveryDraft | undefined {
  if (!record(value) || !id(value.id) || value.id === 0 || !id(value.revision)) return
  const baseline = dnsEditDraftFields(value.baseline), fields = dnsEditDraftFields(value.fields), provider = providerBaseline(value.provider)
  if (!baseline || !fields || provider === undefined || provider && provider.id !== fields.provider_account_id) return
  // DNS edits cannot silently become another domain/provider/type operation.
  if (baseline.domain_name !== fields.domain_name || baseline.provider_account_id !== fields.provider_account_id || baseline.record_type !== fields.record_type) return
  return { id: value.id, revision: value.revision, baseline, fields, provider }
}
export interface CertificateCreateDraftFields {
  node_id: number; provider_account_id: number; name: string; domains: string; contact_email: string
  environment: 'production' | 'staging'; auto_renew: boolean; renew_before_days: number
  challenge_type: 'dns-01' | 'http-01-webroot'; webroot_path: string
}
export function certificateCreateDraftFields(value: unknown): CertificateCreateDraftFields | undefined {
  if (!record(value) || !id(value.node_id) || !id(value.provider_account_id) || !text(value.name, 320) || !text(value.domains, 8192) || !text(value.contact_email, 1024) || !['production', 'staging'].includes(String(value.environment)) || typeof value.auto_renew !== 'boolean' || !integer(value.renew_before_days) || !['dns-01', 'http-01-webroot'].includes(String(value.challenge_type)) || !text(value.webroot_path, 4096)) return
  return { node_id: value.node_id, provider_account_id: value.provider_account_id, name: value.name, domains: value.domains, contact_email: value.contact_email, environment: value.environment as 'production' | 'staging', auto_renew: value.auto_renew, renew_before_days: value.renew_before_days, challenge_type: value.challenge_type as CertificateCreateDraftFields['challenge_type'], webroot_path: value.webroot_path }
}
export interface CertificateCreateRecoveryDraft { fields: CertificateCreateDraftFields; provider: ProviderDraftBaseline | null }
export function certificateCreateRecoveryDraft(value: unknown): CertificateCreateRecoveryDraft | undefined {
  if (!record(value)) return
  const fields = certificateCreateDraftFields(value.fields), provider = providerBaseline(value.provider)
  if (!fields || provider === undefined || provider && provider.id !== fields.provider_account_id) return
  return { fields, provider }
}
export interface CertificateEditDraftFields { name: string; contact_email: string; challenge_type: 'http-01' | 'http-01-webroot' | 'dns-01'; webroot_path: string; auto_renew: boolean; renew_before_days: number }
export function certificateEditDraftFields(value: unknown): CertificateEditDraftFields | undefined {
  if (!record(value) || !text(value.name, 320) || !text(value.contact_email, 1024) || !['http-01', 'http-01-webroot', 'dns-01'].includes(String(value.challenge_type)) || !text(value.webroot_path, 4096) || typeof value.auto_renew !== 'boolean' || !integer(value.renew_before_days)) return
  return { name: value.name, contact_email: value.contact_email, challenge_type: value.challenge_type as CertificateEditDraftFields['challenge_type'], webroot_path: value.webroot_path, auto_renew: value.auto_renew, renew_before_days: value.renew_before_days }
}
export interface CertificateDraftBaseline { fields: CertificateEditDraftFields; node_id: number; provider_account_id: number; domains: string[]; environment: 'production' | 'staging' }
export function certificateDraftBaseline(value: unknown): CertificateDraftBaseline | undefined {
  if (!record(value) || !id(value.node_id) || !id(value.provider_account_id ?? 0) || !Array.isArray(value.domains) || value.domains.length > 10 || value.domains.some(item => !text(item, 1024)) || !['production', 'staging'].includes(String(value.environment))) return
  const fields = certificateEditDraftFields(value)
  if (!fields) return
  return { fields, node_id: value.node_id, provider_account_id: Number(value.provider_account_id || 0), domains: [...value.domains] as string[], environment: value.environment as CertificateDraftBaseline['environment'] }
}
function savedCertificateBaseline(value: unknown): CertificateDraftBaseline | undefined {
  if (!record(value) || !record(value.fields)) return
  return certificateDraftBaseline({ ...value.fields, node_id: value.node_id, provider_account_id: value.provider_account_id, domains: value.domains, environment: value.environment })
}
export interface CertificateEditRecoveryDraft { id: number; revision: number; baseline: CertificateDraftBaseline; fields: CertificateEditDraftFields }
export function certificateEditRecoveryDraft(value: unknown): CertificateEditRecoveryDraft | undefined {
  if (!record(value) || !id(value.id) || value.id === 0 || !id(value.revision)) return
  const fields = certificateEditDraftFields(value.fields), baseline = savedCertificateBaseline(value.baseline)
  if (!fields || !baseline || fields.challenge_type !== baseline.fields.challenge_type) return
  return { id: value.id, revision: value.revision, baseline, fields }
}
export interface CertificateRenewalRecoveryDraft { id: number; revision: number; baseline: CertificateDraftBaseline; fields: { auto_renew: boolean; renew_before_days: number } }
export function certificateRenewalRecoveryDraft(value: unknown): CertificateRenewalRecoveryDraft | undefined {
  if (!record(value) || !id(value.id) || value.id === 0 || !id(value.revision) || !record(value.fields) || typeof value.fields.auto_renew !== 'boolean' || !integer(value.fields.renew_before_days)) return
  const baseline = savedCertificateBaseline(value.baseline)
  if (!baseline) return
  return { id: value.id, revision: value.revision, baseline, fields: { auto_renew: value.fields.auto_renew, renew_before_days: value.fields.renew_before_days } }
}
