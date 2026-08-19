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
  termsUrl: string
  privacyUrl: string
  refundUrl: string
  legalItems: SiteLegalItem[]
  metaTitle: string
  metaDescription: string
  homeKicker: string
  homeTitle: string
  homePrimaryCta: string
}

const defaultDescription = '按流量、速度和设备数选择服务方案，购买后可在用户中心独立管理每一条订阅。'

function configValue(configs: SystemConfig[], key: string) {
  return configs.find(item => item.config_key === key)?.value
}

function stringValue(configs: SystemConfig[], key: string, fallback = '') {
  const value = configValue(configs, key)
  return typeof value === 'string' ? value.trim() || fallback : fallback
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

export function buildSiteProfile(configs: SystemConfig[], fallbackName = 'zboard'): SiteProfile {
  const name = stringValue(configs, 'site_name', fallbackName || 'zboard')
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
    termsUrl: stringValue(configs, 'site_terms_url'),
    privacyUrl: stringValue(configs, 'site_privacy_url'),
    refundUrl: stringValue(configs, 'site_refund_url'),
    legalItems: parseLegalItems(configValue(configs, 'site_legal_items')),
    metaTitle: stringValue(configs, 'site_meta_title', name),
    metaDescription: stringValue(configs, 'site_meta_description', description),
    homeKicker: stringValue(configs, 'site_home_kicker', '灵活套餐 · 独立订阅 · 清晰计费'),
    homeTitle: stringValue(configs, 'site_home_title', '选择适合你的套餐，按需订阅，轻松管理。'),
    homePrimaryCta: stringValue(configs, 'site_home_primary_cta', '浏览套餐'),
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

export function applySiteMetadata(profile: SiteProfile) {
  if (typeof document === 'undefined') return
  document.title = profile.metaTitle || profile.name
  ensureMeta('meta[name="description"]', { name: 'description', content: profile.metaDescription })
  ensureMeta('meta[property="og:site_name"]', { property: 'og:site_name', content: profile.name })
  ensureMeta('meta[property="og:title"]', { property: 'og:title', content: profile.metaTitle || profile.name })
  ensureMeta('meta[property="og:description"]', { property: 'og:description', content: profile.metaDescription })

  const canonicalBase = profile.siteUrl || window.location.origin
  const canonical = absoluteUrl(window.location.pathname, canonicalBase)
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
