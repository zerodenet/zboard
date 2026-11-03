import { onScopeDispose, ref, watch, type Ref } from 'vue'
import { observeTextOverflow } from '../utils/textOverflow'

export function useTextOverflow(element: Ref<HTMLElement | null>, value: () => unknown) {
  const overflowing = ref(false)
  let observation: ReturnType<typeof observeTextOverflow> | undefined
  watch(element, current => {
    observation?.stop()
    observation = current ? observeTextOverflow(current, measured => { overflowing.value = measured }) : undefined
  }, { flush: 'post', immediate: true })
  watch(value, () => observation?.measure(), { flush: 'post' })
  onScopeDispose(() => observation?.stop())
  return overflowing
}
