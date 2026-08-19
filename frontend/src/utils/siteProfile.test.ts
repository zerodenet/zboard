import { describe, expect, it, vi } from 'vitest'
import type { SystemConfig } from '../api/client'
import { buildSiteProfile } from './siteProfile'

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
  it('projects operator branding and legal metadata from public configs', () => {
    const profile = buildSiteProfile([
      config('site_name', 'Example Network'),
      config('site_desc', 'Fast and predictable connectivity.'),
      config('site_logo', 'https://cdn.example.com/logo.svg'),
      config('site_privacy_url', 'https://example.com/privacy'),
      config('site_legal_items', [
        { label: 'Company No.', value: '12345678', url: 'https://registry.example/12345678' },
        { label: '', value: 'ignored' },
      ], 'json'),
    ])

    expect(profile.name).toBe('Example Network')
    expect(profile.description).toBe('Fast and predictable connectivity.')
    expect(profile.logo).toBe('https://cdn.example.com/logo.svg')
    expect(profile.privacyUrl).toBe('https://example.com/privacy')
    expect(profile.legalItems).toEqual([
      { label: 'Company No.', value: '12345678', url: 'https://registry.example/12345678' },
    ])
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
