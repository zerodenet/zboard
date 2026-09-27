import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AdminSystemInfo } from '../api/system'
import AdminVersionStatus from './AdminVersionStatus.vue'

const fetchReleases = vi.hoisted(() => vi.fn())
vi.mock('../api/releaseUpdates', async importOriginal => ({
  ...(await importOriginal<typeof import('../api/releaseUpdates')>()),
  fetchZBoardReleases: fetchReleases,
}))

const info = {
  release_version: 'v0.0.2-rc.202609220931',
  release_channel: 'release-candidate',
  build_time: '2026-09-22T09:31:00Z',
} as AdminSystemInfo

afterEach(() => fetchReleases.mockReset())

describe('AdminVersionStatus', () => {
  it('shows the newer release and its update instructions', async () => {
    fetchReleases.mockResolvedValue([{ tag_name: 'v0.0.2-rc.202609240544', html_url: 'https://github.com/zerodenet/zboard/releases/tag/v0.0.2-rc.202609240544', prerelease: true, draft: false }])
    const wrapper = mount(AdminVersionStatus, { props: { info } })
    await flushPromises()
    expect(wrapper.text()).toContain('发现新版本')
    expect(wrapper.text()).toContain('v0.0.2-rc.202609240544')
    expect(wrapper.get('a[href*="/releases/tag/"]').attributes('target')).toBe('_blank')
    expect(wrapper.get('a[href*="/guides/installation"]').text()).toBe('部署文档')
    wrapper.unmount()
  })

  it('keeps the release link and offers retry when GitHub is unavailable', async () => {
    fetchReleases.mockRejectedValue(new Error('rate limited'))
    const wrapper = mount(AdminVersionStatus, { props: { info } })
    await flushPromises()
    expect(wrapper.text()).toContain('重试检查')
    expect(wrapper.text()).not.toContain('已是最新')
    expect(wrapper.get('a[href="https://github.com/zerodenet/zboard/releases"]')).toBeTruthy()
    wrapper.unmount()
  })
})
