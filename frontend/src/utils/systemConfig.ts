import type { SystemConfig } from '../api/client'
import { isValidTimeZone } from './timeZone'
import { isEmail, isHttpUrl, utf8ByteLength } from './validation'

export type SystemConfigInput = NonNullable<SystemConfig['input']>

const controlLabels: Record<SystemConfigInput['control'], string> = {
  text: '文本',
  textarea: '多行文本',
  url: 'URL',
  email: '邮箱',
  hostname: '主机名',
  password: '密钥',
  integer: '整数',
  port: '端口',
  switch: '开关',
  select: '选项',
  json: 'JSON',
}

const assetUrlConfigKeys = new Set(['site_logo', 'site_logo_dark', 'site_favicon'])
const urlConfigKeys = new Set([
  ...assetUrlConfigKeys,
  'site_support_url',
  'site_telegram_url',
])
const policyContentKeys = new Set(['site_terms_content', 'site_privacy_content', 'site_refund_content'])
const policyContentMaxBytes = 48 * 1024

function validateLegalItems(value: unknown) {
  if (!Array.isArray(value)) return '法律与注册信息必须是 JSON 数组。'
  if (value.length > 32) return '法律与注册信息最多添加 32 项。'
  for (let index = 0; index < value.length; index += 1) {
    const item = value[index]
    if (!item || typeof item !== 'object' || Array.isArray(item)) return `第 ${index + 1} 项必须是对象。`
    const row = item as Record<string, unknown>
    if (typeof row.label !== 'string' || !row.label.trim()) return `第 ${index + 1} 项缺少标签。`
    if (utf8ByteLength(row.label.trim()) > 120) return `第 ${index + 1} 项标签不能超过 120 个 UTF-8 字节。`
    if (typeof row.value !== 'string' || !row.value.trim()) return `第 ${index + 1} 项缺少值。`
    if (utf8ByteLength(row.value.trim()) > 512) return `第 ${index + 1} 项值不能超过 512 个 UTF-8 字节。`
    if (row.url !== undefined && row.url !== '' && (typeof row.url !== 'string' || !isHttpUrl(row.url.trim()))) {
      return `第 ${index + 1} 项的链接必须是完整 HTTP 或 HTTPS 地址。`
    }
    if (typeof row.url === 'string' && utf8ByteLength(row.url.trim()) > 2048) return `第 ${index + 1} 项链接不能超过 2048 个 UTF-8 字节。`
  }
  if (utf8ByteLength(JSON.stringify(value)) > 16 * 1024) return '法律与注册信息总大小不能超过 16 KiB。'
  return ''
}

export function resolveSystemConfigInput(config: SystemConfig): SystemConfigInput {
  // Site customization is a product-level contract. Keep these controls and
  // limits authoritative even while older backends still return the generic
  // SystemConfig input schema for newly introduced keys.
  if (config.config_key === 'site_support_email') return { control: 'email', max_bytes: 254, placeholder: 'support@example.com' }
  if (urlConfigKeys.has(config.config_key)) return { control: 'url', max_bytes: 2048, placeholder: assetUrlConfigKeys.has(config.config_key) ? 'https://cdn.example.com/logo.svg' : 'https://example.com/…' }
  if (policyContentKeys.has(config.config_key)) return { control: 'textarea', max_bytes: policyContentMaxBytes }
  if (config.config_key === 'site_desc') return { control: 'textarea', max_bytes: 500 }
  if (config.config_key === 'site_footer_copyright') return { control: 'textarea', max_bytes: 1024 }
  if (config.config_key === 'site_meta_description' || config.config_key === 'site_home_title') return { control: 'textarea', max_bytes: 1024 }
  if (config.config_key === 'site_meta_title') return { control: 'text', max_bytes: 180 }
  if (config.config_key === 'site_home_kicker') return { control: 'text', max_bytes: 120 }
  if (config.config_key === 'site_legal_items') return { control: 'json', max_bytes: 16 * 1024 }

  if (config.input?.control) return config.input
  if (config.config_key === 'system_timezone') return { control: 'text', required: true, placeholder: 'Asia/Shanghai' }
  if (config.is_secret) return { control: 'password' }
  if (config.value_type === 'bool') return { control: 'switch' }
  if (config.value_type === 'int') return { control: 'integer', step: 1 }
  if (config.value_type === 'json') return { control: 'json' }
  return { control: 'text' }
}

export function systemConfigControlLabel(config: SystemConfig) {
  return controlLabels[resolveSystemConfigInput(config).control]
}

export function formatSystemConfigDraft(config: SystemConfig) {
  if (config.is_secret) return ''
  if (config.value_type === 'json') {
    return config.value === undefined ? '' : JSON.stringify(config.value, null, 2)
  }
  return config.value
}

export function normalizeSystemConfigDraft(
  config: SystemConfig,
  draft: unknown,
): { value?: unknown; error?: string } {
  const input = resolveSystemConfigInput(config)
  if (input.control === 'switch') return { value: Boolean(draft) }

  if (input.control === 'integer' || input.control === 'port') {
    const value = typeof draft === 'number' ? draft : Number(String(draft ?? '').trim())
    if (!Number.isSafeInteger(value)) return { error: input.control === 'port' ? '请输入有效端口。' : '请输入有效整数。' }
    if (input.min !== undefined && value < input.min) return { error: `数值不能小于 ${input.min}。` }
    if (input.max !== undefined && value > input.max) return { error: `数值不能大于 ${input.max}。` }
    return { value }
  }

  if (input.control === 'json') {
    let value = draft
    if (typeof draft === 'string') {
      try {
        value = JSON.parse(draft)
      } catch {
        return { error: '请输入有效 JSON。' }
      }
    }
    if (config.config_key === 'site_legal_items') {
      const error = validateLegalItems(value)
      if (error) return { error }
    }
    if (input.max_bytes !== undefined && utf8ByteLength(JSON.stringify(value)) > input.max_bytes) {
      return { error: `${config.name}不能超过 ${input.max_bytes} 个 UTF-8 字节。` }
    }
    return { value }
  }

  const value = String(draft ?? '').trim()
  if (input.required && value === '') return { error: `${config.name}不能为空。` }
  if (input.max_bytes !== undefined && utf8ByteLength(value) > input.max_bytes) {
    const readableLimit = input.max_bytes === policyContentMaxBytes ? '48 KiB' : `${input.max_bytes} 个 UTF-8 字节`
    return { error: `${config.name}不能超过 ${readableLimit}。` }
  }
  if (value === '') return { value }

  if (config.config_key === 'system_timezone' && !isValidTimeZone(value)) {
    return { error: '请输入有效的 IANA 时区，例如 Asia/Shanghai、UTC 或 America/Los_Angeles。' }
  }
  if (input.control === 'url' && !isHttpUrl(value)) {
    return { error: '请输入不含账号、密码或片段的完整 HTTP 或 HTTPS 地址。' }
  }
  if (input.control === 'email' && !isEmail(value, input.max_bytes || 254)) {
    return { error: '请输入有效邮箱地址。' }
  }
  if (input.control === 'hostname' && (!/^[a-zA-Z0-9.-]+$/.test(value) || value.includes('..'))) {
    return { error: '请输入不含协议、端口或空格的有效主机名。' }
  }
  if (input.control === 'select' && !(input.options || []).some(option => option.value === value)) {
    return { error: '请选择有效选项。' }
  }
  return { value }
}
