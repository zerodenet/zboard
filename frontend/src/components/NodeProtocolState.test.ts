import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NodeProtocolState from './NodeProtocolState.vue'

describe('node protocol business and configuration facts', () => {
  it('does not turn an enabled record or a missing queue into a running listener', () => {
    const view = mount(NodeProtocolState, { props: { isActive: true } })
    expect(view.text()).toContain('已启用')
    expect(view.text()).toContain('配置交付状态未知')
    expect(view.text()).not.toMatch(/运行中|已可用/)
  })

  it.each([
    ['queued', '配置交付待处理'],
    ['running', '配置交付处理中'],
    ['reconciling', '配置交付结果待核验'],
    ['deferred', '配置交付受能力限制'],
    ['failed', '配置交付失败，待恢复'],
    ['suspended', '配置交付已暂停'],
    ['future_unknown', '配置交付状态未知'],
  ])('keeps current %s separate from an older successful publication', (status, label) => {
    const view = mount(NodeProtocolState, { props: { isActive: true, publication: { status }, latestDeployment: { status: 'succeeded' } } })
    expect(view.text()).toContain(label)
    expect(view.text()).not.toContain('最近配置发布已确认')
    expect(view.text()).not.toContain('运行中')
  })

  it('does not report stopped old connections when configuration alone is confirmed', () => {
    const view = mount(NodeProtocolState, { props: { isActive: false, publication: { status: 'connections_pending' } } })
    expect(view.text()).toContain('已停用')
    expect(view.text()).toContain('配置已应用；旧连接撤权待核验')
    expect(view.text()).not.toContain('连接已断开')
  })

  it('labels a retained success as a recent configuration fact without claiming live availability', async () => {
    const view = mount(NodeProtocolState, { props: { isActive: true, latestDeployment: { status: 'succeeded' } } })
    expect(view.text()).toContain('最近配置发布已确认')
    expect(view.text()).not.toMatch(/运行中|已可用/)
    await view.setProps({ latestDeployment: null })
    expect(view.text()).toContain('配置交付状态未知')
  })
})
