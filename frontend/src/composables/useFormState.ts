import { computed, isRef, nextTick, onBeforeUnmount, onMounted, reactive, ref, type Ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { confirmAction } from '../utils/feedback'
import { normalizeApiFormError } from '../utils/apiError'

function snapshot(value: unknown): string {
  return JSON.stringify(value, (_key, item) => item === undefined ? null : item)
}

export function useDirtyForm(readValue: () => unknown) {
  const baseline = ref(snapshot(readValue()))
  const dirty = computed(() => snapshot(readValue()) !== baseline.value)

  function markClean() {
    baseline.value = snapshot(readValue())
  }

  async function confirmDiscard(options: {
    title?: string
    message?: string
    confirmText?: string
  } = {}) {
    if (!dirty.value) return true
    return confirmAction({
      title: options.title || '放弃未保存的修改？',
      message: options.message || '当前表单包含尚未保存的内容，离开后这些修改将丢失。',
      confirmText: options.confirmText || '放弃修改',
      tone: 'danger',
    })
  }

  return { dirty, markClean, confirmDiscard }
}

export function useUnsavedChangesGuard(
  isDirty: () => boolean,
  confirmLeave: () => boolean | Promise<boolean>,
) {
  const dirty = computed(isDirty)

  async function confirmNavigation() {
    if (!dirty.value) return true
    return confirmLeave()
  }

  function handleBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty.value) return
    event.preventDefault()
    event.returnValue = true
  }

  onMounted(() => window.addEventListener('beforeunload', handleBeforeUnload))
  onBeforeUnmount(() => window.removeEventListener('beforeunload', handleBeforeUnload))
  onBeforeRouteLeave(confirmNavigation)

  return { dirty, confirmLeave: confirmNavigation }
}

export function useFormErrors() {
  const fields = reactive<Record<string, string>>({})
  const formError = ref('')

  function clear(name?: string) {
    if (name) {
      delete fields[name]
      if (Object.keys(fields).length === 0) formError.value = ''
      return
    }
    for (const key of Object.keys(fields)) delete fields[key]
    formError.value = ''
  }

  function set(next: Record<string, string>, message = '') {
    clear()
    Object.assign(fields, next)
    formError.value = message
  }

  async function focusFirst(root: Ref<HTMLElement | null> | HTMLElement | null) {
    await nextTick()
    const element = isRef(root) ? root.value : root
    const control = element?.querySelector<HTMLElement>('[aria-invalid="true"], [data-form-error]')
    control?.focus()
  }

  async function applyValidation(
    next: Record<string, string>,
    root: Ref<HTMLElement | null> | HTMLElement | null = null,
    message = '请更正标记字段后再继续。',
  ) {
    const valid = Object.keys(next).length === 0
    set(next, valid ? '' : message)
    if (!valid) await focusFirst(root)
    return valid
  }

  async function applyApiError(
    cause: unknown,
    fallback: string,
    root: Ref<HTMLElement | null> | HTMLElement | null = null,
    fieldMap: Record<string, string> = {},
  ) {
    const normalized = normalizeApiFormError(cause, fallback, fieldMap)
    set(normalized.fields, normalized.message)
    if (Object.keys(normalized.fields).length) await focusFirst(root)
    return normalized
  }

  return { fields, formError, clear, set, focusFirst, applyValidation, applyApiError }
}
