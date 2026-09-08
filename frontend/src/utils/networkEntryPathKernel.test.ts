import { execFileSync } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, it } from 'vitest'
import { buildPathNode, emptyPathNode, pathProtocolOptions, type PathProtocol } from './networkEntryPath'

it.runIf(Boolean(process.env.ZBOARD_ZERO_VALIDATE_BIN))('validates every form protocol through the actual Zero kernel', () => {
  const dir = mkdtempSync(join(tmpdir(), 'zboard-entry-path-'))
  try {
    for (const { value } of pathProtocolOptions) {
      const path = buildPathNode({ ...emptyPathNode(), protocol: value as PathProtocol, server: 'proxy.example.com', port: 443, password: 'test-password', id: '550e8400-e29b-41d4-a716-446655440000', cipher: value === 'vmess' ? 'aes-128-gcm' : 'chacha20-ietf-poly1305' })
      const config = { inbounds: [{ tag: 'entry', listen: { address: '127.0.0.1', port: 12345 }, protocol: { type: 'direct', target: 'landing.example.com', port: 443 } }], outbounds: path.outbounds, outbound_groups: path.outbound_groups, route: { final: { type: 'route', outbound: path.target } } }
      const file = join(dir, `${value}.json`)
      writeFileSync(file, JSON.stringify(config))
      expect(() => execFileSync(process.env.ZBOARD_ZERO_VALIDATE_BIN!, ['validate', file], { timeout: 15000, stdio: 'pipe' }), value).not.toThrow()
    }
  } finally { rmSync(dir, { recursive: true, force: true }) }
}, 120000)
