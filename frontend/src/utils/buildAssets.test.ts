import { expect, it } from 'vitest'
import { verifyBuildAssets, versionedAssetsDirectory } from '../../buildAssets'

function build(version = '0.0.3-dev.202610160859') {
  const directory = versionedAssetsDirectory(version)
  const main = `${directory}/index.js`, route = `${directory}/Home.js`, css = `${directory}/PublicLayout.css`
  const bundle = {
    'index.html': { type: 'asset' as const, fileName: 'index.html', source: `<script type="module" src="/${main}"></script>` },
    [main]: { type: 'chunk' as const, fileName: main, dynamicImports: [route] },
    [route]: { type: 'chunk' as const, fileName: route, viteMetadata: { importedCss: new Set([css]), importedAssets: new Set<string>() } },
    [css]: { type: 'asset' as const, fileName: css, source: '.page{}' },
  }
  return { directory, bundle, css, main, route }
}

it('isolates unchanged CSS and lazy chunks between releases', () => {
  const previous = build(), next = build('0.0.3-dev.202610160900')
  verifyBuildAssets(previous.directory, previous.bundle)
  verifyBuildAssets(next.directory, next.bundle)
  expect(previous.css).not.toBe(next.css)
  expect(previous.route).not.toBe(next.route)
})

it('rejects a build that references missing lazy-route CSS', () => {
  const output = build()
  delete output.bundle[output.css]
  expect(() => verifyBuildAssets(output.directory, output.bundle)).toThrow('PublicLayout.css')
})

it('rejects a missing lazy module or an entry pointing at an older release', () => {
  const output = build()
  delete output.bundle[output.route]
  expect(() => verifyBuildAssets(output.directory, output.bundle)).toThrow('Home.js')
  const valid = build()
  valid.bundle['index.html'].source = '<script src="/assets/old/index.js"></script>'
  expect(() => verifyBuildAssets(valid.directory, valid.bundle)).toThrow('assets/old/index.js')
})

it('rejects invalid version paths before building public URLs', () => {
  for (const version of ['', '../assets', '0.0.3/dev', '0.0.3?cache=old']) {
    expect(() => versionedAssetsDirectory(version)).toThrow()
  }
})
