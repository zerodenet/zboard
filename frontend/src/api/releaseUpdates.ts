const RELEASES_API = 'https://api.github.com/repos/zerodenet/zboard/releases'
const CACHE_KEY = 'zboard-releases-v1'
const CACHE_MS = 15 * 60 * 1000
export const ZBOARD_RELEASES_URL = 'https://github.com/zerodenet/zboard/releases'
export const ZBOARD_DEPLOYMENT_GUIDE_URL = 'https://docs.zerodenet.org/projects/zboard/guides/installation'

export interface ZBoardRelease {
  tag_name: string
  html_url: string
  prerelease: boolean
  draft: boolean
  published_at: string | null
}

type Version = { parts: number[]; stage: number; suffix: number[] }

function parseVersion(value: string): Version | null {
  const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-(dev|alpha|beta|rc)(?:\.(\d+(?:\.\d+)*))?)?$/i.exec(value.trim())
  if (!match) return null
  const ranks: Record<string, number> = { dev: 0, alpha: 1, beta: 2, rc: 3 }
  return {
    parts: [Number(match[1]), Number(match[2]), Number(match[3])],
    stage: match[4] ? ranks[match[4].toLowerCase()] : 4,
    suffix: match[5] ? match[5].split('.').map(Number) : [],
  }
}

export function compareZBoardVersions(left: string, right: string): number | null {
  const a = parseVersion(left)
  const b = parseVersion(right)
  if (!a || !b) return null
  for (let i = 0; i < 3; i++) {
    if (a.parts[i] !== b.parts[i]) return Math.sign(a.parts[i] - b.parts[i])
  }
  if (a.stage !== b.stage) return Math.sign(a.stage - b.stage)
  for (let i = 0; i < Math.max(a.suffix.length, b.suffix.length); i++) {
    const delta = (a.suffix[i] ?? -1) - (b.suffix[i] ?? -1)
    if (delta) return Math.sign(delta)
  }
  return 0
}

function eligible(currentChannel: string, release: ZBoardRelease): boolean {
  const version = parseVersion(release.tag_name)
  if (!version || release.draft) return false
  if (currentChannel === 'stable') return version.stage === 4 && !release.prerelease
  if (currentChannel === 'release-candidate') return version.stage >= 3
  if (currentChannel === 'preview') return version.stage >= 1
  return true
}

export function latestEligibleRelease(releases: ZBoardRelease[], currentVersion: string, channel: string): ZBoardRelease | null {
  if (!parseVersion(currentVersion)) return null
  return releases
    .filter(release => eligible(channel, release) && compareZBoardVersions(release.tag_name, currentVersion)! > 0)
    .reduce<ZBoardRelease | null>((latest, release) => !latest || compareZBoardVersions(release.tag_name, latest.tag_name)! > 0 ? release : latest, null)
}

export function releaseChannelForVersion(value: string): string {
  const stage = parseVersion(value)?.stage
  if (stage === 4) return 'stable'
  if (stage === 3) return 'release-candidate'
  if (stage === 1 || stage === 2) return 'preview'
  return 'development'
}

function isZBoardRelease(item: unknown): item is ZBoardRelease {
  if (!item || typeof item !== 'object') return false
  const release = item as Partial<ZBoardRelease>
  return typeof release.tag_name === 'string'
    && typeof release.html_url === 'string'
    && release.html_url.startsWith(`${ZBOARD_RELEASES_URL}/tag/`)
    && typeof release.prerelease === 'boolean'
    && typeof release.draft === 'boolean'
}

export async function fetchZBoardReleases(channel: string, force = false): Promise<ZBoardRelease[]> {
  const cacheKey = `${CACHE_KEY}-${channel}`
  if (!force) {
    try {
      const cached = JSON.parse(sessionStorage.getItem(cacheKey) || 'null') as { at?: number; releases?: unknown[] } | null
      if (cached?.at && Date.now() - cached.at < CACHE_MS && Array.isArray(cached.releases) && cached.releases.length && cached.releases.every(isZBoardRelease)) return cached.releases
    } catch { /* Ignore unavailable or invalid browser storage. */ }
  }
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 8000)
  try {
    const response = await fetch(channel === 'stable' ? `${RELEASES_API}/latest` : `${RELEASES_API}?per_page=100`, {
      signal: controller.signal,
      headers: { Accept: 'application/vnd.github+json' },
    })
    if (!response.ok) throw new Error(`GitHub HTTP ${response.status}`)
    const payload: unknown = await response.json()
    const releases = (Array.isArray(payload) ? payload : [payload]).filter(isZBoardRelease)
    if (!releases.length) throw new Error('GitHub 未返回可用的版本信息')
    try { sessionStorage.setItem(cacheKey, JSON.stringify({ at: Date.now(), releases })) } catch { /* Cache is optional. */ }
    return releases
  } finally {
    clearTimeout(timeout)
  }
}
