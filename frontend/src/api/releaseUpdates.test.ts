import { afterEach, describe, expect, it, vi } from 'vitest'
import { compareZBoardVersions, fetchZBoardReleases, latestEligibleRelease, releaseChannelForVersion, type ZBoardRelease } from './releaseUpdates'

const release = (tag_name: string, prerelease = tag_name.includes('-')): ZBoardRelease => ({
  tag_name,
  prerelease,
  draft: false,
  published_at: '2026-09-24T00:00:00Z',
  html_url: `https://github.com/zerodenet/zboard/releases/tag/${tag_name}`,
})

afterEach(() => vi.unstubAllGlobals())

describe('ZBoard release update selection', () => {
  it('compares RC timestamps numerically and stable tags after RCs', () => {
    expect(compareZBoardVersions('v0.0.2-rc.202609240544', 'v0.0.2-rc.202609220931')).toBeGreaterThan(0)
    expect(compareZBoardVersions('v0.0.2', 'v0.0.2-rc.202609240544')).toBeGreaterThan(0)
    expect(compareZBoardVersions('v0.0.1', 'v0.0.2-rc.202609240544')).toBeLessThan(0)
    expect(compareZBoardVersions('unknown', 'v0.0.2')).toBeNull()
  })

  it('follows the installed release channel and ignores drafts and older releases', () => {
    const draft = { ...release('v0.0.4'), draft: true }
    const releases = [release('v0.0.2-rc.202609240544'), release('v0.0.3-rc.1'), release('v0.0.2'), release('v0.0.1'), draft]
    expect(latestEligibleRelease(releases, 'v0.0.2-rc.202609240544', 'release-candidate')?.tag_name).toBe('v0.0.3-rc.1')
    expect(latestEligibleRelease(releases, 'v0.0.1', 'stable')?.tag_name).toBe('v0.0.2')
    expect(latestEligibleRelease(releases, 'v0.0.2', 'stable')).toBeNull()
    expect(releaseChannelForVersion('v0.0.2-rc.202609240544')).toBe('release-candidate')
  })

  it('queries the latest stable release directly and keeps prereleases in the RC feed', async () => {
    const request = vi.fn(async (_input: string) => ({ ok: true, json: async (): Promise<ZBoardRelease | ZBoardRelease[]> => release('v0.0.2', false) }))
    vi.stubGlobal('fetch', request)
    expect((await fetchZBoardReleases('stable', true))[0]?.tag_name).toBe('v0.0.2')
    expect(request.mock.calls[0]?.[0]).toBe('https://api.github.com/repos/zerodenet/zboard/releases/latest')
    request.mockImplementation(async () => ({ ok: true, json: async () => [release('v0.0.2-rc.1')] }))
    expect((await fetchZBoardReleases('release-candidate', true))[0]?.tag_name).toBe('v0.0.2-rc.1')
    expect(request.mock.calls[1]?.[0]).toBe('https://api.github.com/repos/zerodenet/zboard/releases?per_page=100')
  })
})
