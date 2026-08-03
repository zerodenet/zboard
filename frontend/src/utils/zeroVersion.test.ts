import { describe, expect, it } from 'vitest'
import { compareZeroVersions, zeroVersionAtLeast } from './zeroVersion'

describe('Zero version ordering', () => {
  it('orders a formal release after every prerelease of the same version', () => {
    expect(compareZeroVersions('0.0.15', '0.0.15-rc.4')).toBe(1)
    expect(compareZeroVersions('v0.0.15', '0.0.15-rc.10')).toBe(1)
  })

  it('orders numeric prerelease identifiers numerically', () => {
    expect(compareZeroVersions('0.0.15-rc.2', '0.0.15-rc.10')).toBe(-1)
    expect(compareZeroVersions('0.0.15-rc.4', '0.0.15-rc.4')).toBe(0)
  })

  it('fails closed for an unknown installed version', () => {
    expect(zeroVersionAtLeast('', '0.0.15-rc.4')).toBe(false)
    expect(zeroVersionAtLeast('development', '0.0.15-rc.4')).toBe(false)
  })
})
