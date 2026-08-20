import { describe, expect, it } from 'vitest'
import { isRemoteLegalContent, renderSafeMarkdown, resolveLegalVariables, stripLeadingLegalTitle } from './legalContent'
import type { SiteProfile } from './siteProfile'

const profile: SiteProfile = {
  name: 'Example Network',
  description: 'Example description',
  siteUrl: 'https://example.com',
  logo: '',
  logoDark: '',
  favicon: '',
  copyright: '© 2026 Example Network',
  supportEmail: 'support@example.com',
  supportUrl: 'https://example.com/support',
  telegramUrl: '',
  termsContent: '',
  privacyContent: '',
  refundContent: '',
  policyDocuments: [],
  legalItems: [],
  metaTitle: 'Example Network',
  metaDescription: 'Example description',
  homeKicker: '',
  homeTitle: '',
}

describe('legal content utilities', () => {
  it('treats only a standalone http(s) URL as remote content', () => {
    expect(isRemoteLegalContent('https://example.com/terms')).toBe(true)
    expect(isRemoteLegalContent('  https://example.com/terms  ')).toBe(true)
    expect(isRemoteLegalContent('http://example.com/privacy')).toBe(true)
    expect(isRemoteLegalContent('https://example.com\nextra')).toBe(false)
    expect(isRemoteLegalContent('# Terms')).toBe(false)
    expect(isRemoteLegalContent('javascript:alert(1)')).toBe(false)
  })

  it('resolves site variables at display time', () => {
    expect(resolveLegalVariables('{{site_name}} · {{copyright}} · {{support_email}} · {{support_contact}}', profile))
      .toBe('Example Network · © 2026 Example Network · support@example.com · support@example.com')
  })

  it('falls back from support email to the configured support URL', () => {
    expect(resolveLegalVariables('{{support_contact}}', { ...profile, supportEmail: '' }))
      .toBe('https://example.com/support')
    expect(resolveLegalVariables('{{support_contact}}', { ...profile, supportEmail: '', supportUrl: '' }))
      .toBe('站点提供的客服入口')
  })

  it('renders a bounded markdown subset and escapes raw html', () => {
    const html = renderSafeMarkdown('# Terms\n\n**Bold** <script>alert(1)</script>\n\n[Safe](https://example.com) [Bad](javascript:alert(1))')
    expect(html).toContain('<h1>Terms</h1>')
    expect(html).toContain('<strong>Bold</strong>')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(html).toContain('href="https://example.com/"')
    expect(html).not.toContain('href="javascript:')
  })

  it('removes only a matching leading markdown title', () => {
    expect(stripLeadingLegalTitle('# 服务条款\n\n正文', '服务条款')).toBe('正文')
    expect(stripLeadingLegalTitle('# 其他标题\n\n正文', '服务条款')).toBe('# 其他标题\n\n正文')
    expect(stripLeadingLegalTitle('https://example.com/terms', '服务条款')).toBe('https://example.com/terms')
  })
})
