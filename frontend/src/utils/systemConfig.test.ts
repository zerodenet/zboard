import { describe, expect, it } from 'vitest'
import type { SystemConfig } from '../api/client'
import {
  formatSystemConfigDraft,
  normalizeSystemConfigDraft,
  resolveSystemConfigInput,
  systemConfigControlLabel,
} from './systemConfig'

function config(overrides: Partial<SystemConfig>): SystemConfig {
  return {
    id: 1,
    config_key: 'test',
    name: '测试配置',
    value: '',
    value_type: 'string',
    description: '',
    is_public: false,
    is_secret: false,
    configured: false,
    revision: 1,
    updated_at: '2026-07-23T00:00:00Z',
    ...overrides,
  }
}

describe('system config schema', () => {
  it('uses the server control schema for generic settings and keeps a compatibility fallback', () => {
    const port = config({ input: { control: 'port', min: 1, max: 65535 } })
    expect(resolveSystemConfigInput(port).control).toBe('port')
    expect(systemConfigControlLabel(port)).toBe('端口')
    expect(resolveSystemConfigInput(config({ value_type: 'bool' })).control).toBe('switch')
    expect(resolveSystemConfigInput(config({ value_type: 'json' })).control).toBe('json')
  })

  it('keeps site policy limits authoritative over a legacy generic server schema', () => {
    const policy = config({
      config_key: 'site_terms_content',
      name: '服务条款',
      is_public: true,
      input: { control: 'text', max_bytes: 100000 },
    })
    expect(resolveSystemConfigInput(policy)).toMatchObject({ control: 'textarea', max_bytes: 48 * 1024 })
    expect(normalizeSystemConfigDraft(policy, 'a'.repeat(48 * 1024))).toEqual({ value: 'a'.repeat(48 * 1024) })
    expect(normalizeSystemConfigDraft(policy, 'a'.repeat(48 * 1024 + 1)).error).toContain('48 KiB')
  })

  it('keeps site contact and asset controls typed over a legacy generic server schema', () => {
    const email = config({ config_key: 'site_support_email', input: { control: 'text', max_bytes: 100000 } })
    expect(resolveSystemConfigInput(email).control).toBe('email')
    expect(normalizeSystemConfigDraft(email, 'support@example.com')).toEqual({ value: 'support@example.com' })
    expect(normalizeSystemConfigDraft(email, 'not-an-email')).toEqual({ error: '请输入有效邮箱地址。' })

    const logo = config({ config_key: 'site_logo_dark', input: { control: 'text', max_bytes: 100000 } })
    expect(resolveSystemConfigInput(logo).control).toBe('url')
    expect(normalizeSystemConfigDraft(logo, 'https://cdn.example.com/logo.svg')).toEqual({ value: 'https://cdn.example.com/logo.svg' })
    expect(normalizeSystemConfigDraft(logo, '/brand/logo.svg')).toEqual({
      error: '请输入不含账号、密码或片段的完整 HTTP 或 HTTPS 地址。',
    })
  })

  it('validates structured legal item limits before save', () => {
    const legal = config({ config_key: 'site_legal_items', value_type: 'json', input: { control: 'json' } })
    const tooMany = Array.from({ length: 33 }, (_, index) => ({ label: 'ID', value: String(index) }))
    expect(normalizeSystemConfigDraft(legal, JSON.stringify(tooMany))).toEqual({ error: '法律与注册信息最多添加 32 项。' })
  })

  it('validates the dynamic policy document collection before save', () => {
    const documents = config({ config_key: 'site_policy_documents', name: '政策文档中心', value_type: 'json' })
    const valid = [{ slug: 'fair-use', title: '公平使用政策', summary: '', content: '# 公平使用政策', published: true, placements: ['footer', 'purchase'] }]
    expect(normalizeSystemConfigDraft(documents, JSON.stringify(valid))).toEqual({ value: valid })
    expect(normalizeSystemConfigDraft(documents, 'null')).toEqual({ value: null })
    expect(normalizeSystemConfigDraft(documents, JSON.stringify([...valid, { ...valid[0] }])).error).toContain('不能重复')
    expect(normalizeSystemConfigDraft(documents, JSON.stringify([{ ...valid[0], placements: ['checkout'] }])).error).toContain('无效展示位置')
  })

  it('provides a required IANA timezone input', () => {
    const timezone = config({ config_key: 'system_timezone', name: '系统时区', value: 'UTC' })
    expect(resolveSystemConfigInput(timezone)).toMatchObject({ control: 'text', required: true, placeholder: 'Asia/Shanghai' })
    expect(normalizeSystemConfigDraft(timezone, 'Asia/Shanghai')).toEqual({ value: 'Asia/Shanghai' })
    expect(normalizeSystemConfigDraft(timezone, 'Not/A_Real_Zone')).toEqual({
      error: '请输入有效的 IANA 时区，例如 Asia/Shanghai、UTC 或 America/Los_Angeles。',
    })
  })

  it('formats JSON for editing and never exposes a configured secret value', () => {
    expect(formatSystemConfigDraft(config({ value_type: 'json', value: { enabled: true } }))).toBe('{\n  "enabled": true\n}')
    expect(formatSystemConfigDraft(config({ is_secret: true, configured: true, value: 'ciphertext' }))).toBe('')
  })

  it('normalizes typed controls and reports actionable local errors', () => {
    const port = config({ name: 'SMTP 端口', input: { control: 'port', min: 1, max: 65535 } })
    expect(normalizeSystemConfigDraft(port, 587)).toEqual({ value: 587 })
    expect(normalizeSystemConfigDraft(port, 70000)).toEqual({ error: '数值不能大于 65535。' })

    const tls = config({ input: { control: 'select', options: [{ label: 'STARTTLS', value: 'starttls' }] } })
    expect(normalizeSystemConfigDraft(tls, 'plain')).toEqual({ error: '请选择有效选项。' })

    const url = config({ input: { control: 'url' } })
    expect(normalizeSystemConfigDraft(url, 'https://user@example.com/#secret')).toEqual({
      error: '请输入不含账号、密码或片段的完整 HTTP 或 HTTPS 地址。',
    })

    const json = config({ value_type: 'json', input: { control: 'json' } })
    expect(normalizeSystemConfigDraft(json, '{"enabled":true}')).toEqual({ value: { enabled: true } })
    expect(normalizeSystemConfigDraft(json, '{bad')).toEqual({ error: '请输入有效 JSON。' })
  })
})
