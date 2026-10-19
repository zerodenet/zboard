const maximumInput = 9007199254740991n
export function parseWalletAmount(value: string): number | null {
  const text = value.trim()
  if (!/^\d+(?:\.\d{1,2})?$/.test(text)) return null
  const [whole, fraction = ''] = text.split('.')
  const minor = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, '0'))
  return minor > 0n && minor <= maximumInput ? Number(minor) : null
}
export function walletMajor(value: string | number): string {
  const amount = BigInt(value), absolute = amount < 0n ? -amount : amount
  return `${amount < 0n ? '-' : ''}${absolute / 100n}.${String(absolute % 100n).padStart(2, '0')}`
}
export function formatWalletMoney(value: string, currency: string): string {
  if (!/^-?\d+$/.test(value)) return '金额未知'
  return `${currency} ${walletMajor(value)}`
}
