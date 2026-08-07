import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function readSource(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('entity reference read models', () => {
  it('shows subscription names in the account traffic page', () => {
    const source = readSource('./account/AccountTraffic.vue')
    expect(source).toContain('WorkbenchFilterSelect')
    expect(source).toContain('EntityReference')
    expect(source).not.toContain('label="订阅 ID"')
    expect(source).not.toContain("{{ record.subscription_id || '—' }}")
  })

  it('uses entity references in admin traffic and reconciliation tables', () => {
    const source = readSource('./Traffic.vue')
    expect(source).toContain('fetchAdminEntityReferences')
    expect(source).toContain('userReference(record.user_id)')
    expect(source).toContain('subscriptionReference(item.subscription_id)')
    expect(source).not.toContain("{{ record.user_id || '—' }}")
    expect(source).not.toContain('{{ item.plan_id }}')
  })

  it('resolves audit targets and associated users', () => {
    const source = readSource('./AuditLogs.vue')
    expect(source).toContain('targetReference(item.target)')
    expect(source).toContain('detailUserReference')
    expect(source).not.toContain("<code>{{ detail.target || '—' }}</code>")
    expect(source).not.toContain('`#${detail.user_id}`')
  })
})
