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

export function resolveSystemConfigInput(config: SystemConfig): SystemConfigInput {
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
    if (typeof draft !== 'string') return { value: draft }
    try {
      return { value: JSON.parse(draft) }
    } catch {
      return { error: '请输入有效 JSON。' }
    }
  }

  const value = String(draft ?? '').trim()
  if (input.required && value === '') return { error: `${config.name}不能为空。` }
  if (input.max_bytes !== undefined && utf8ByteLength(value) > input.max_bytes) {
    return { error: `${config.name}不能超过 ${input.max_bytes} 个 UTF-8 字节。` }
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
