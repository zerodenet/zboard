import { describe, expect, it, vi } from 'vitest'
import type { SystemConfig } from '../api/client'
import { applySiteMetadata, buildSiteProfile } from './siteProfile'

function config(config_key: string, value: unknown, value_type: SystemConfig['value_type'] = 'string'): SystemConfig {
  return {
    id: 1,
    config_key,
    name: config_key,
    value,
    value_type,
    description: '',
    is_public: true,
    is_secret: false,
    configured: true,
    revision: 1,
    updated_at: '2026-08-19T00:00:00Z',
  }
}

describe('buildSiteProfile', () => {
  it('projects operator branding, policy content and legal metadata from public configs', () => {
    const profile = buildSiteProfile([
      config('site_name', 'Example Network'),
      config('site_desc', 'Fast and predictable connectivity.'),
      config('site_logo', 'https://cdn.example.com/logo.svg'),
      config('site_privacy_content', '# Privacy\n\n{{copyright}}'),
      config('site_legal_items', [
        { label: 'Company No.', value: '12345678', url: 'https://registry.example/12345678' },
        { label: '', value: 'ignored' },
      ], 'json'),
    ])

    expect(profile.name).toBe('Example Network')
    expect(profile.description).toBe('Fast and predictable connectivity.')
    expect(profile.logo).toBe('https://cdn.example.com/logo.svg')
    expect(profile.privacyContent).toBe('# Privacy\n\n{{copyright}}')
    expect(profile.policyDocuments.find(document => document.slug === 'privacy')?.content).toBe('# Privacy\n\n{{copyright}}')
    expect(profile.legalItems).toEqual([
      { label: 'Company No.', value: '12345678', url: 'https://registry.example/12345678' },
    ])
  })

  it('uses the dynamic document collection when configured and preserves an explicitly empty collection', () => {
    const documents = [{ slug: 'fair-use', title: '公平使用政策', summary: '资源使用边界', content: '# 公平使用政策', published: true, placements: ['footer', 'purchase'] }]
    expect(buildSiteProfile([config('site_policy_documents', documents, 'json')]).policyDocuments).toEqual(documents)
    expect(buildSiteProfile([config('site_policy_documents', [], 'json'), config('site_terms_content', '# Terms')]).policyDocuments).toEqual([])
  })

  it('keeps compatibility with URL-only policy values', () => {
    const profile = buildSiteProfile([
      config('site_terms_url', 'https://example.com/terms'),
    ])
    expect(profile.termsContent).toBe('https://example.com/terms')
  })

  it('keeps useful defaults when optional customization is empty', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-19T00:00:00Z'))
    const profile = buildSiteProfile([], 'My Service')
    expect(profile.name).toBe('My Service')
    expect(profile.copyright).toBe('© 2026 My Service')
    expect(profile.metaTitle).toBe('My Service')
    expect(profile.legalItems).toEqual([])
    vi.useRealTimers()
  })
})

describe('applySiteMetadata', () => {
  it('uses the configured SEO title on home and route-aware titles/canonical URLs elsewhere', () => {
    const profile = buildSiteProfile([
      config('site_name', 'Example Network'),
      config('site_url', 'https://example.com'),
      config('site_meta_title', 'Reliable Network Service'),
    ])

    applySiteMetadata(profile, { path: '/', pageTitle: '首页' })
    expect(document.title).toBe('Reliable Network Service')
    expect(document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href).toBe('https://example.com/')

    applySiteMetadata(profile, { path: '/pricing', pageTitle: '套餐价格' })
    expect(document.title).toBe('套餐价格 · Example Network')
    expect(document.head.querySelector<HTMLMetaElement>('meta[property="og:title"]')?.content).toBe('套餐价格 · Example Network')
    expect(document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href).toBe('https://example.com/pricing')
    expect(document.head.querySelector<HTMLMetaElement>('meta[property="og:url"]')?.content).toBe('https://example.com/pricing')
  })
})
