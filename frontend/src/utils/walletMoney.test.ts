import { describe, expect, it } from 'vitest'
import { parseWalletAmount, walletMajor, formatWalletMoney } from './walletMoney'
describe('wallet integer money', () => {
  it('parses decimal minor units exactly and rejects fractions below one cent', () => {
    expect(parseWalletAmount('0.29')).toBe(29)
    expect(parseWalletAmount('120.00')).toBe(12000)
    for (const value of ['0', '-1', '1.001', '1e3', 'NaN', '90071992547409.92']) expect(parseWalletAmount(value)).toBeNull()
    expect(parseWalletAmount('90071992547409.91')).toBe(9007199254740991)
  })
  it('formats cumulative balances above the JavaScript number range without rounding them', () => {
    expect(walletMajor('90071992547409910')).toBe('900719925474099.10')
    expect(formatWalletMoney('-29', 'CNY')).toBe('CNY -0.29')
    expect(formatWalletMoney('90071992547409910', 'CNY')).toBe('CNY 900719925474099.10')
    expect(formatWalletMoney('unknown', 'CNY')).toBe('金额未知')
  })
})
