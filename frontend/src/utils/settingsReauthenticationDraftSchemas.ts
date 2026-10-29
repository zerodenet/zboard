// View-specific allowlists. Never serialize the Settings page's complete drafts/configs.
const isRecord = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown, max: number): value is string => typeof value === 'string' && new TextEncoder().encode(value).byteLength <= max
const revision = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0
export const recoverableSiteKeys = ['site_logo', 'site_logo_dark', 'site_favicon', 'site_desc', 'site_home_kicker', 'site_home_title', 'site_footer_copyright', 'site_support_email', 'site_support_url', 'site_telegram_url', 'site_meta_title', 'site_meta_description'] as const
// These are public content, never arbitrary JSON configuration or SMTP drafts.
export const recoverableLegalKeys = ['site_policy_documents', 'site_legal_items', 'site_terms_content', 'site_privacy_content', 'site_refund_content'] as const
const recoverableSettingsKeys: readonly string[] = [...recoverableSiteKeys, ...recoverableLegalKeys]
export function settingsRecoveryValueType(key: string): 'string' | 'json' | undefined {
  if (key === 'site_policy_documents' || key === 'site_legal_items') return 'json'
  if (recoverableSettingsKeys.includes(key)) return 'string'
}
function publicContentDraft(key: string, value: unknown): value is string {
  // Recovery is not submission validation. Empty fields, duplicate slugs and
  // invalid public URLs stay editable; only structural allowlists are checked.
  // The shared entry/total byte limit rejects large drafts without truncating.
  if (!text(value, 512 * 1024)) return false
  if (settingsRecoveryValueType(key) !== 'json') return true
  if (!value.trim()) return true
  let rows: unknown
  try { rows = JSON.parse(value) } catch { return false }
  if (rows === null) return true
  if (!Array.isArray(rows) || rows.length > 32) return false
  return rows.every(row => {
    if (!isRecord(row)) return false
    if (key === 'site_legal_items') {
      return Object.keys(row).every(field => ['label', 'value', 'url'].includes(field))
        && typeof row.label === 'string' && typeof row.value === 'string'
        && (row.url === undefined || typeof row.url === 'string')
    }
    return Object.keys(row).every(field => ['slug', 'title', 'summary', 'content', 'published', 'placements'].includes(field))
      && typeof row.slug === 'string' && typeof row.title === 'string' && typeof row.content === 'string'
      && (row.summary === undefined || typeof row.summary === 'string')
      && typeof row.published === 'boolean'
      && (row.placements === undefined || Array.isArray(row.placements) && row.placements.length <= 3 && row.placements.every(placement => ['footer', 'auth', 'purchase'].includes(placement)))
  })
}
function settingsConfigText(key: string, value: unknown): value is string {
  return recoverableLegalKeys.includes(key as typeof recoverableLegalKeys[number]) ? publicContentDraft(key, value) : text(value, 16384)
}
export interface SiteBaseline { site_name: string; site_url: string; allow_registration: boolean }
export function siteBaseline(value: unknown): SiteBaseline | undefined {
  if (!isRecord(value) || !text(value.site_name, 320) || !text(value.site_url, 8192) || typeof value.allow_registration !== 'boolean') return
  return { site_name: value.site_name, site_url: value.site_url, allow_registration: value.allow_registration }
}
interface SiteEdit { site_name: string; site_url: string; baseline: SiteBaseline }
interface ConfigEdit { key: string; value: string; baseline: string; revision: number }
export interface SettingsRecoveryDraft { site: SiteEdit | null; configs: ConfigEdit[] }
export function settingsRecoveryDraft(value: unknown): SettingsRecoveryDraft | undefined {
  if (!isRecord(value) || !Array.isArray(value.configs) || value.configs.length > recoverableSettingsKeys.length) return
  let site: SiteEdit | null = null
  if (value.site !== null) {
    if (!isRecord(value.site) || !text(value.site.site_name, 320) || !text(value.site.site_url, 8192)) return
    const baseline = siteBaseline(value.site.baseline)
    if (!baseline) return
    site = { site_name: value.site.site_name, site_url: value.site.site_url, baseline }
  }
  const configs: ConfigEdit[] = []
  for (const item of value.configs) {
    if (!isRecord(item) || typeof item.key !== 'string' || !recoverableSettingsKeys.includes(item.key) || !settingsConfigText(item.key, item.value) || !settingsConfigText(item.key, item.baseline) || !revision(item.revision) || configs.some(entry => entry.key === item.key)) return
    configs.push({ key: String(item.key), value: item.value, baseline: item.baseline, revision: item.revision })
  }
  return { site, configs }
}
export interface RegistrationRecoveryDraft {
  registration: { value: boolean; baseline: SiteBaseline } | null
  verification: { value: boolean; baseline: boolean; revision: number } | null
}
export function registrationRecoveryDraft(value: unknown): RegistrationRecoveryDraft | undefined {
  if (!isRecord(value)) return
  let registration: RegistrationRecoveryDraft['registration'] = null
  let verification: RegistrationRecoveryDraft['verification'] = null
  if (value.registration !== null) {
    if (!isRecord(value.registration) || typeof value.registration.value !== 'boolean') return
    const baseline = siteBaseline(value.registration.baseline)
    if (!baseline) return
    registration = { value: value.registration.value, baseline }
  }
  if (value.verification !== null) {
    const item = value.verification
    if (!isRecord(item) || typeof item.value !== 'boolean' || typeof item.baseline !== 'boolean' || !revision(item.revision)) return
    verification = { value: item.value, baseline: item.baseline, revision: item.revision }
  }
  return { registration, verification }
}
export interface EmailTemplateFields { name: string; slug: string; category: 'registration' | 'operational'; subject_template: string; body_template: string; is_active: boolean; sort_order: number }
export function emailTemplateFields(value: unknown): EmailTemplateFields | undefined {
  if (!isRecord(value) || !text(value.name, 320) || !text(value.slug, 320) || !text(value.subject_template, 800) || !text(value.body_template, 100000) || (value.category !== 'registration' && value.category !== 'operational') || typeof value.is_active !== 'boolean' || !Number.isSafeInteger(value.sort_order)) return
  return { name: value.name, slug: value.slug, category: value.category as EmailTemplateFields['category'], subject_template: value.subject_template, body_template: value.body_template, is_active: value.is_active, sort_order: Number(value.sort_order) }
}
export interface EmailTemplateRecoveryDraft { id: number; revision: number; fields: EmailTemplateFields; baseline: EmailTemplateFields | null }
export function emailTemplateRecoveryDraft(value: unknown): EmailTemplateRecoveryDraft | undefined {
  if (!isRecord(value) || !revision(value.id) || !revision(value.revision)) return
  const fields = emailTemplateFields(value.fields), baseline = value.baseline === null ? null : emailTemplateFields(value.baseline)
  if (!fields || baseline === undefined || (value.id > 0 && !baseline) || (value.id === 0 && (value.revision !== 0 || baseline !== null || fields.category !== 'operational'))) return
  return { id: value.id, revision: value.revision, fields, baseline }
}
