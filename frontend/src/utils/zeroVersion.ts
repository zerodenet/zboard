type ParsedZeroVersion = {
  core: [number, number, number]
  prerelease: Array<number | string>
}

function parseZeroVersion(value: string): ParsedZeroVersion | null {
  const normalized = value.trim().replace(/^v/i, '')
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/.exec(normalized)
  if (!match) return null
  return {
    core: [Number(match[1]), Number(match[2]), Number(match[3])],
    prerelease: match[4]
      ? match[4].split('.').map(part => /^\d+$/.test(part) ? Number(part) : part)
      : [],
  }
}

export function compactZeroVersion(value?: string | null): string {
  const normalized = value?.trim() || ''
  if (!normalized) return ''
  const parsed = parseZeroVersion(normalized)
  return parsed ? `v${parsed.core.join('.')}` : normalized
}

export function compareZeroVersions(left: string, right: string): number {
  const a = parseZeroVersion(left)
  const b = parseZeroVersion(right)
  if (!a || !b) return 0
  for (let index = 0; index < a.core.length; index++) {
    if (a.core[index] !== b.core[index]) return a.core[index] < b.core[index] ? -1 : 1
  }
  if (a.prerelease.length === 0 || b.prerelease.length === 0) {
    if (a.prerelease.length === b.prerelease.length) return 0
    return a.prerelease.length === 0 ? 1 : -1
  }
  for (let index = 0; index < Math.max(a.prerelease.length, b.prerelease.length); index++) {
    const av = a.prerelease[index]
    const bv = b.prerelease[index]
    if (av === undefined || bv === undefined) return av === undefined ? -1 : 1
    if (av === bv) continue
    if (typeof av === 'number' && typeof bv === 'number') return av < bv ? -1 : 1
    if (typeof av === 'number') return -1
    if (typeof bv === 'number') return 1
    return av < bv ? -1 : 1
  }
  return 0
}

export function zeroVersionAtLeast(version: string, minimum: string): boolean {
  return parseZeroVersion(version) !== null &&
    parseZeroVersion(minimum) !== null &&
    compareZeroVersions(version, minimum) >= 0
}
