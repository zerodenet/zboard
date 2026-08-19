import type { SystemConfig } from '../api/client'

export interface SiteLegalItem {
  label: string
  value: string
  url?: string
}

export interface SiteProfile {
  name: string
  description: string
  siteUrl: string
  logo: string
  logoDark: string
  favicon: string
  copyright: string
  supportEmail: string
  supportUrl: string
  telegramUrl: string
  termsContent: string
  privacyContent: string
  refundContent: string
  legalItems: SiteLegalItem[]
  metaTitle: string
  metaDescription: string
  homeKicker: string
  homeTitle: string
}

export interface SiteMetadataContext {
  path?: string
  pageTitle?: string
}

const defaultDescription = '按流量、速度和设备数选择服务方案，购买后可在用户中心独立管理每一条订阅。'

function configValue(configs: SystemConfig[], key: string) {
  return configs.find(item => item.config_key === key)?.value
}

function stringValue(configs: SystemConfig[], key: string, fallback = '') {
  const value = configValue(configs, key)
  return typeof value === 'string' ? value.trim() || fallback : fallback
}

function contentValue(configs: SystemConfig[], key: string, legacyKey: string) {
  return stringValue(configs, key, stringValue(configs, legacyKey))
}

function parseLegalItems(value: unknown): SiteLegalItem[] {
  let source = value
  if (typeof source === 'string') {
    try { source = JSON.parse(source) } catch { return [] }
  }
  if (!Array.isArray(source)) return []
  return source.flatMap(item => {
    if (!item || typeof item !== 'object') return []
    const row = item as Record<string, unknown>
    const label = typeof row.label === 'string' ? row.label.trim() : ''
    const value = typeof row.value === 'string' ? row.value.trim() : ''
    const url = typeof row.url === 'string' ? row.url.trim() : ''
    if (!label || !value) return []
    return [{ label, value, ...(url ? { url } : {}) }]
  })
}

// preferredName is authoritative when supplied by Installation or an unsaved
// settings draft. Public site_name remains the fallback for standalone callers.
export function buildSiteProfile(configs: SystemConfig[], preferredName = ''): SiteProfile {
  const name = preferredName.trim() || stringValue(configs, 'site_name', 'zboard')
  const description = stringValue(configs, 'site_desc', defaultDescription)
  return {
    name,
    description,
    siteUrl: stringValue(configs, 'site_url'),
    logo: stringValue(configs, 'site_logo'),
    logoDark: stringValue(configs, 'site_logo_dark'),
    favicon: stringValue(configs, 'site_favicon'),
    copyright: stringValue(configs, 'site_footer_copyright', `© ${new Date().getFullYear()} ${name}`),
    supportEmail: stringValue(configs, 'site_support_email'),
    supportUrl: stringValue(configs, 'site_support_url'),
    telegramUrl: stringValue(configs, 'site_telegram_url'),
    termsContent: contentValue(configs, 'site_terms_content', 'site_terms_url'),
    privacyContent: contentValue(configs, 'site_privacy_content', 'site_privacy_url'),
    refundContent: contentValue(configs, 'site_refund_content', 'site_refund_url'),
    legalItems: parseLegalItems(configValue(configs, 'site_legal_items')),
    metaTitle: stringValue(configs, 'site_meta_title', name),
    metaDescription: stringValue(configs, 'site_meta_description', description),
    homeKicker: stringValue(configs, 'site_home_kicker', '灵活套餐 · 独立订阅 · 清晰计费'),
    homeTitle: stringValue(configs, 'site_home_title', '选择适合你的套餐，按需订阅，轻松管理。'),
  }
}

function ensureMeta(selector: string, attrs: Record<string, string>) {
  let element = document.head.querySelector<HTMLMetaElement>(selector)
  if (!element) {
    element = document.createElement('meta')
    document.head.appendChild(element)
  }
  for (const [key, value] of Object.entries(attrs)) element.setAttribute(key, value)
}

function ensureLink(rel: string) {
  let element = document.head.querySelector<HTMLLinkElement>(`link[rel="${rel}"]`)
  if (!element) {
    element = document.createElement('link')
    element.rel = rel
    document.head.appendChild(element)
  }
  return element
}

function absoluteUrl(value: string, base: string) {
  if (!value) return ''
  try { return new URL(value, base || window.location.origin).toString() } catch { return '' }
}

function routeDocumentTitle(profile: SiteProfile, context: SiteMetadataContext) {
  const path = context.path || (typeof window !== 'undefined' ? window.location.pathname : '/')
  const pageTitle = context.pageTitle?.trim() || ''
  if (path !== '/' && pageTitle) return `${pageTitle} · ${profile.name}`
  return profile.metaTitle || profile.name
}

export function applySiteMetadata(profile: SiteProfile, context: SiteMetadataContext = {}) {
  if (typeof document === 'undefined') return
  const title = routeDocumentTitle(profile, context)
  document.title = title
  ensureMeta('meta[name="description"]', { name: 'description', content: profile.metaDescription })
  ensureMeta('meta[property="og:site_name"]', { property: 'og:site_name', content: profile.name })
  ensureMeta('meta[property="og:title"]', { property: 'og:title', content: title })
  ensureMeta('meta[property="og:description"]', { property: 'og:description', content: profile.metaDescription })

  const canonicalBase = profile.siteUrl || window.location.origin
  const path = context.path || window.location.pathname
  const canonical = absoluteUrl(path, canonicalBase)
  if (canonical) {
    ensureLink('canonical').href = canonical
    ensureMeta('meta[property="og:url"]', { property: 'og:url', content: canonical })
  }
  if (profile.favicon) ensureLink('icon').href = absoluteUrl(profile.favicon, canonicalBase) || profile.favicon
  if (profile.logo) {
    const image = absoluteUrl(profile.logo, canonicalBase)
    if (image) ensureMeta('meta[property="og:image"]', { property: 'og:image', content: image })
  }
}
