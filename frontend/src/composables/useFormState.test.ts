import { defineComponent, h, nextTick, reactive, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { feedbackState, settleConfirm } from '../utils/feedback'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from './useFormState'

beforeEach(() => {
  while (feedbackState.confirm) settleConfirm(false)
})

describe('useDirtyForm', () => {
  it('tracks the saved baseline and confirms before discarding changes', async () => {
    const form = reactive({ name: 'before' })
    const state = useDirtyForm(() => form)
    expect(state.dirty.value).toBe(false)

    form.name = 'after'
    expect(state.dirty.value).toBe(true)
    const decision = state.confirmDiscard()
    expect(feedbackState.confirm?.title).toBe('放弃未保存的修改？')
    settleConfirm(true)
    await expect(decision).resolves.toBe(true)

    state.markClean()
    expect(state.dirty.value).toBe(false)
    await expect(state.confirmDiscard()).resolves.toBe(true)
  })
})

describe('useUnsavedChangesGuard', () => {
  it('protects route navigation and browser unload only while changes are dirty', async () => {
    const dirty = ref(false)
    const confirmLeave = vi.fn(async () => false)
    const GuardedPage = defineComponent({
      setup() {
        useUnsavedChangesGuard(() => dirty.value, confirmLeave)
        return () => h('div', 'guarded')
      },
    })
    const NextPage = defineComponent({ setup: () => () => h('div', 'next') })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: GuardedPage },
        { path: '/next', component: NextPage },
      ],
    })
    await router.push('/')
    await router.isReady()
    const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), {
      global: { plugins: [router] },
    })

    const cleanUnload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(cleanUnload)
    expect(cleanUnload.defaultPrevented).toBe(false)

    dirty.value = true
    await nextTick()
    const dirtyUnload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirtyUnload)
    expect(dirtyUnload.defaultPrevented).toBe(true)

    await router.push('/next')
    expect(confirmLeave).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/')

    confirmLeave.mockResolvedValueOnce(true)
    await router.push('/next')
    expect(router.currentRoute.value.path).toBe('/next')

    wrapper.unmount()
    const afterUnmount = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(afterUnmount)
    expect(afterUnmount.defaultPrevented).toBe(false)
  })
})

describe('useFormErrors', () => {
  it('replaces and clears field and form errors', () => {
    const errors = useFormErrors()
    errors.set({ email: '邮箱格式不正确' }, '提交失败')
    expect(errors.fields.email).toBe('邮箱格式不正确')
    expect(errors.formError.value).toBe('提交失败')
    errors.clear('email')
    expect(errors.fields.email).toBeUndefined()
    expect(errors.formError.value).toBe('')
  })

  it('keeps the form summary until the last field error is corrected', () => {
    const errors = useFormErrors()
    errors.set({ email: '邮箱格式不正确', password: '密码长度不足' }, '请更正标记字段。')

    errors.clear('email')
    expect(errors.fields.password).toBe('密码长度不足')
    expect(errors.formError.value).toBe('请更正标记字段。')

    errors.clear('password')
    expect(errors.formError.value).toBe('')
  })

  it('applies versioned API fields through a declared form mapping', async () => {
    const errors = useFormErrors()
    await errors.applyApiError({ response: { data: {
      message: '校验失败。',
      error: { version: 1, code: 'validation_failed', fields: { admin_email: '邮箱无效。' } },
    } } }, '提交失败。', null, { admin_email: 'email' })
    expect(errors.fields.email).toBe('邮箱无效。')
    expect(errors.formError.value).toBe('校验失败。')
  })

  it('applies local validation and focuses the first invalid control', async () => {
    const errors = useFormErrors()
    const root = document.createElement('div')
    const input = document.createElement('input')
    input.setAttribute('aria-invalid', 'true')
    root.append(input)
    document.body.append(root)

    await expect(errors.applyValidation({ name: '请输入名称。' }, root)).resolves.toBe(false)
    expect(errors.fields.name).toBe('请输入名称。')
    expect(errors.formError.value).toBe('请更正标记字段后再继续。')
    expect(document.activeElement).toBe(input)

    await expect(errors.applyValidation({}, root)).resolves.toBe(true)
    expect(errors.formError.value).toBe('')
    root.remove()
  })
})
