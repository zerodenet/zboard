import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import RowActions from './RowActions.vue'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('RowActions', () => {
  it('renders one available action directly', () => {
    const wrapper = mount(RowActions, {
      slots: { default: '<button type="button">查看</button>' },
    })

    expect(wrapper.attributes('data-row-action-mode')).toBe('single')
    expect(wrapper.get('button').text()).toBe('查看')
    expect(wrapper.find('[aria-haspopup="menu"]').exists()).toBe(false)
  })

  it('collapses multiple actions into one accessible popup trigger', async () => {
    const wrapper = mount(RowActions, {
      attachTo: document.body,
      props: { label: '协议服务的操作', triggerKey: 'protocol-1' },
      slots: {
        default: '<button type="button">查看</button><button type="button">编辑</button>',
      },
    })

    expect(wrapper.attributes('data-row-action-mode')).toBe('menu')
    expect(wrapper.attributes('data-row-action-count')).toBe('2')
    const trigger = wrapper.get('[data-row-action-trigger="protocol-1"]')
    expect(trigger.attributes('aria-label')).toBe('协议服务的操作')

    await trigger.trigger('click')
    const menu = document.body.querySelector('[role="menu"]')
    expect(menu?.textContent).toContain('查看')
    expect(menu?.textContent).toContain('编辑')
    expect(menu?.querySelectorAll('[role="menuitem"]')).toHaveLength(2)
    wrapper.unmount()
  })
})
