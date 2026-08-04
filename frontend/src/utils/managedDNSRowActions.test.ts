import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const managedDNS = readFileSync(resolve(process.cwd(), 'src/views/ManagedDNS.vue'), 'utf8')

describe('managed DNS row actions', () => {
  it('uses the shared row-action menu instead of a wrapping inline button group', () => {
    expect(managedDNS).toContain("import RowActions from '../components/RowActions.vue'")
    expect(managedDNS).toContain('<RowActions :label="`${record.record_type} ${record.domain_name} 的操作`"')
    expect(managedDNS).toContain(':trigger-key="`dns-${record.id}`"')
    expect(managedDNS).not.toContain('class="inline-actions"')
    expect(managedDNS).not.toContain('.inline-actions')
  })

  it('keeps destructive deletion inside the shared action menu', () => {
    const menuStart = managedDNS.indexOf('<RowActions')
    const menuEnd = managedDNS.indexOf('</RowActions>', menuStart)
    const menu = managedDNS.slice(menuStart, menuEnd)

    expect(menu).toContain('variant="danger"')
    expect(menu).toContain('@click="removeRecord(record)"')
  })
})
