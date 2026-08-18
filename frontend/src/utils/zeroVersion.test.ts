import { describe, expect, it } from 'vitest'
import { compactZeroVersion, compareZeroVersions, zeroVersionAtLeast } from './zeroVersion'

describe('Zero version ordering', () => {
  it('orders a formal release after every prerelease of the same version', () => {
    expect(compareZeroVersions('0.0.15', '0.0.15-rc.4')).toBe(1)
    expect(compareZeroVersions('v0.0.15', '0.0.15-rc.10')).toBe(1)
  })

  it('orders numeric prerelease identifiers numerically', () => {
    expect(compareZeroVersions('0.0.15-rc.2', '0.0.15-rc.10')).toBe(-1)
    expect(compareZeroVersions('0.0.15-rc.4', '0.0.15-rc.4')).toBe(0)
  })

  it('compacts long build versions without losing the original comparison semantics', () => {
    expect(compactZeroVersion('v0.0.16-dev.202608140314')).toBe('v0.0.16')
    expect(compactZeroVersion('0.12.3-rc.4')).toBe('v0.12.3')
    expect(compactZeroVersion('development')).toBe('development')
  })

  it('fails closed for an unknown installed version', () => {
    expect(zeroVersionAtLeast('', '0.0.15-rc.4')).toBe(false)
    expect(zeroVersionAtLeast('development', '0.0.15-rc.4')).toBe(false)
  })
})
