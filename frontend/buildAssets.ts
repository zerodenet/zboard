type Artifact = {
  type: 'asset' | 'chunk'
  fileName: string
  source?: string | Uint8Array
  imports?: string[]
  dynamicImports?: string[]
  viteMetadata?: { importedCss: Set<string>; importedAssets: Set<string> }
}

export function versionedAssetsDirectory(version: string): string {
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error('A valid package version is required for frontend assets')
  }
  return `assets/${version}`
}

/** Verify the emitted graph, including lazy-route CSS, before packaging it. */
export function verifyBuildAssets(directory: string, bundle: Record<string, Artifact>): void {
  function requireAsset(name: string) {
    if (!name.startsWith(`${directory}/`) || !bundle[name]) {
      throw new Error(`Missing or unversioned frontend dependency: ${name}`)
    }
  }
  for (const artifact of Object.values(bundle)) {
    if (artifact.type !== 'chunk') continue
    requireAsset(artifact.fileName)
    for (const name of [
      ...(artifact.imports || []), ...(artifact.dynamicImports || []),
      ...(artifact.viteMetadata?.importedCss || []), ...(artifact.viteMetadata?.importedAssets || []),
    ]) {
      if (!/^(?:https?:|data:)/.test(name)) requireAsset(name)
    }
  }
  const entry = bundle['index.html']
  if (!entry || typeof entry.source !== 'string') throw new Error('Frontend entry document is missing')
  for (const match of entry.source.matchAll(/(?:src|href)=["']\/(assets\/[^"']+)["']/g)) {
    requireAsset(match[1])
  }
}
