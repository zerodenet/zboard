import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = join(import.meta.dirname, '..')
const views = join(root, 'views')

describe('infrastructure detail density policy', () => {
  it('uses a comparison table for node protocol multipliers', () => {
    const nodes = readFileSync(join(views, 'Nodes.vue'), 'utf8')

    expect(nodes).toContain('caption="当前节点协议服务与计费倍率"')
    expect(nodes).toContain(':row-count="nodeProtocolTotal"')
    expect(nodes).not.toContain('<article v-for="endpoint in nodeEndpoints"')
  })

  it('uses a comparison table for protocol deployment history', () => {
    const protocols = readFileSync(join(views, 'Protocols.vue'), 'utf8')

    expect(protocols).toContain('caption="协议服务发布历史"')
    expect(protocols).toContain(':row-count="deploymentTotal"')
    expect(protocols).not.toContain('<article v-for="item in deployments"')
  })

  it('keeps node detail operation feedback inside the drawer', () => {
    const nodes = readFileSync(join(views, 'Nodes.vue'), 'utf8')
    const drawerStart = nodes.indexOf('<DetailDrawer')
    const drawerEnd = nodes.indexOf('</DetailDrawer>')
    const drawer = nodes.slice(drawerStart, drawerEnd)

    expect(drawer).toContain(':success="detailMessage"')
    expect(drawer).toContain(':error="detailError"')
    expect(nodes).toContain("detailError.value = e?.response?.data?.message || 'SSH 验证失败。'")
    expect(nodes).toContain("detailError.value = e?.response?.data?.message || 'Zero 内核检测失败。'")
    expect(nodes).toContain("detailError.value = e?.response?.data?.message || '计费倍率保存失败。'")
  })

  it('separates provider accounts and managed DNS into independent infrastructure pages', () => {
    const providers = readFileSync(join(views, 'Providers.vue'), 'utf8')
    const dns = readFileSync(join(views, 'ManagedDNS.vue'), 'utf8')
    const layout = readFileSync(join(root, 'layouts', 'AdminLayout.vue'), 'utf8')
    const router = readFileSync(join(root, 'router', 'index.ts'), 'utf8')

    expect(providers.match(/class="provider-section panel"/g)).toHaveLength(1)
    expect(providers).not.toContain('fetchManagedDNSRecordsPage')
    expect(providers).not.toContain('title="DNS 解析"')
    expect(providers).toContain('.provider-empty-state { min-height: 150px; padding: 24px; }')
    expect(dns).toContain('title="DNS 解析"')
    expect(dns).toContain('fetchManagedDNSRecordsPage')
    expect(dns).toContain('to="/admin/providers"')
    expect(layout).toContain("{ to: '/admin/providers', label: '外部供应商' }")
    expect(layout).toContain("{ to: '/admin/dns-records', label: 'DNS 解析' }")
    expect(layout).toContain("label: '资源与接入'")
    expect(router).toContain("path: 'dns-records'")
  })

  it('requires an explicit release selection and downgrade confirmation for node kernel changes', () => {
    const nodes = readFileSync(join(views, 'Nodes.vue'), 'utf8')

    expect(nodes).toContain('v-model="selectedReleaseVersion"')
    expect(nodes).toContain('fetchZeroReleases()')
    expect(nodes).toContain('const latestPublishedRelease = computed(() => zeroReleases.value[0] || null)')
    expect(nodes).toContain('selectedReleaseVersion.value = zeroReleases.value.find(releaseCompatible)?.version ||')
    expect(nodes).not.toContain('没有可安装的稳定版本')
    expect(nodes).toContain("title: downgrade ? '确认降级 Zero 内核' : '对齐 Zero 内核'")
    expect(nodes).toContain('reconcileNodeKernel(nodeID, { version: selectedRelease.value.version, allow_downgrade: downgrade })')
  })
})
