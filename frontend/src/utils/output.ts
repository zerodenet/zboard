export function normalizeOutput(value?: string | null) {
  return String(value || '')
    .replace(/\x1B(?:\[[0-?]*[ -/]*[@-~]|[@-_])/g, '')
    .replace(/\r\n?/g, '\n')
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, '\uFFFD')
    .normalize('NFC')
    .trim()
}

export function truncateOutput(value: string, maxLength: number) {
  return value.length > maxLength ? `${value.slice(0, maxLength)}\n…` : value
}
