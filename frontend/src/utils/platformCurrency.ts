// Defaults never relabel an existing SKU, order, wallet or persisted intent.
export function platformDefaultCurrency(configs?: readonly { config_key: string; value?: unknown }[]): string {
  const raw = configs?.find(config => config.config_key === 'default_currency')?.value
  const currency = typeof raw === 'string' ? raw.trim().toUpperCase() : ''
  return /^[A-Z]{3}$/.test(currency) ? currency : 'CNY'
}

export function platformCurrencyOptions(configs: readonly { config_key: string; value?: unknown; input?: { options?: { label: string; value: string }[] } }[] | undefined, current: string) {
  const options = configs?.find(config => config.config_key === 'default_currency')?.input?.options || []
  const values = new Set([platformDefaultCurrency(configs), current.trim().toUpperCase(), ...options.map(option => option.value)])
  return [...values].filter(value => /^[A-Z]{3}$/.test(value)).map(value => ({ label: value, value }))
}
