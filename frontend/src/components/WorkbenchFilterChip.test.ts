import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

describe('filter popover layout lifecycle', () => {
  const wrappers: VueWrapper[] = []
  let panelHeight: number
  let resize: () => void
  const observe = vi.fn(), disconnect = vi.fn()
  beforeEach(() => {
    panelHeight = 100
    observe.mockReset(); disconnect.mockReset()
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: () => void) { resize = callback }
      observe = observe
      disconnect = disconnect
    })
    vi.stubGlobal('innerHeight', 720)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('workbench-filter-popover')
        ? { width: 520, height: panelHeight } as DOMRect
        : { left: 200, top: 350, bottom: 380, width: 100, height: 30 } as DOMRect
    })
  })
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    document.getElementById('fixture-select-menu')?.remove()
    vi.restoreAllMocks(); vi.unstubAllGlobals()
  })
  function create() {
    const wrapper = mount(WorkbenchFilterChip, {
      attachTo: document.body, props: { label: '订阅', wide: true },
      slots: { default: '<button aria-controls="fixture-select-menu">每页</button>' },
      global: { plugins: [PrimeVue] },
    })
    wrappers.push(wrapper)
    return wrapper
  }
  async function open(wrapper: VueWrapper) {
    await wrapper.get('.workbench-filter-chip-trigger').trigger('click')
    await flushPromises()
    return document.querySelector<HTMLElement>('[role="dialog"]')!
  }
  it('repositions after asynchronous options increase the panel height', async () => {
    const panel = await open(create())
    expect(panel.style.top).toBe('386px')
    panelHeight = 704; resize(); await flushPromises()
    expect(panel.style.top).toBe('8px')
    expect(observe).toHaveBeenCalledWith(panel)
  })
  it('retains internal scrolling but closes when the surrounding page scrolls', async () => {
    const panel = await open(create())
    panel.querySelector('.workbench-filter-popover-body')!.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBe(panel)
    window.dispatchEvent(new Event('scroll')); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(disconnect).toHaveBeenCalledOnce()
  })
  it('allows pointer and scroll events from an owned teleported select menu', async () => {
    const panel = await open(create())
    const menu = document.createElement('div')
    menu.id = 'fixture-select-menu'; document.body.appendChild(menu)
    menu.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    menu.dispatchEvent(new Event('scroll')); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBe(panel)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('disconnects the observer when unmounted while open', async () => {
    const wrapper = create(); await open(wrapper)
    wrappers.pop(); wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
    resize(); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('does not create an observer if the panel closes before its next render', async () => {
    const wrapper = create()
    const vm = wrapper.vm as unknown as { show: () => Promise<void>; close: () => void }
    const showing = vm.show(); vm.close(); await showing
    expect(observe).not.toHaveBeenCalled()
  })
  it('gives each filter its own dialog and label identifiers', () => {
    const first = create(), second = create()
    expect(first.get('button').attributes('aria-controls')).not.toBe(second.get('button').attributes('aria-controls'))
  })
})
