import { expect, it } from 'vitest'
import { platformDefaultCurrency, platformCurrencyOptions } from './platformCurrency'
it('uses configured defaults without guessing from currency symbols or mutating records', () => {
 expect(platformDefaultCurrency()).toBe('CNY')
 expect(platformDefaultCurrency([])).toBe('CNY')
 expect(platformDefaultCurrency([{ config_key:'default_currency',value:' usd ' }])).toBe('USD')
 expect(platformDefaultCurrency([{ config_key:'default_currency',value:'¥' }])).toBe('CNY')
})

it('uses server choices while retaining a historical or unresolved currency', () => {
  const configs = [{ config_key: 'default_currency', value: 'USD', input: { options: [{ label: '美元', value: 'USD' }, { label: '欧元', value: 'EUR' }] } }]
  expect(platformCurrencyOptions(configs, 'CNY').map(option => option.value)).toEqual(['USD', 'CNY', 'EUR'])
  expect(platformCurrencyOptions(undefined, 'JPY').map(option => option.value)).toEqual(['CNY', 'JPY'])
})
