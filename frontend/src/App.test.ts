import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App.vue'
import { frontendLoadFailure } from './utils/frontendLoadRecovery'

const route = vi.hoisted(() => ({ path: '/', matched: [] as object[] }))
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('./stores/app', () => ({ useAppStore: () => ({
  maintenance: { enabled: false },
  isAdmin: false,
  isAuthenticated: true,
  announcements: [],
  loadSystemStatus: vi.fn().mockResolvedValue(undefined),
}) }))

afterEach(() => {
  frontendLoadFailure.value = null
  route.matched = []
})

function render() {
  return mount(App, { global: {
    components: { UiButton: { template: '<button><slot /></button>' } },
    stubs: { RouterView: true, MaintenanceScreen: true, AnnouncementStack: true, NavigationProgress: true, FeedbackHost: true },
  } })
}

it('replaces the initial endless loader with a working reload action on asset failure', () => {
  frontendLoadFailure.value = 'assets'
  const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => undefined)
  const wrapper = render()
  expect(wrapper.find('.app-boot').exists()).toBe(false)
  expect(wrapper.get('[role="alert"]').text()).toContain('页面资源加载失败')
  expect(wrapper.find('.app-load-failure-initial').exists()).toBe(true)
  wrapper.get('button').trigger('click')
  expect(reload).toHaveBeenCalledOnce()
  wrapper.unmount()
})

it('lets the user retain the existing page after a failed navigation', async () => {
  route.matched = [{}]
  frontendLoadFailure.value = 'assets'
  const wrapper = render()
  expect(wrapper.find('.app-load-failure-initial').exists()).toBe(false)
  expect(wrapper.find('router-view-stub').exists()).toBe(true)
  await wrapper.findAll('button')[1].trigger('click')
  expect(frontendLoadFailure.value).toBeNull()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  wrapper.unmount()
})
