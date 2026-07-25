<template>
  <PrimeButton
    v-bind="forwardedAttrs"
    :class="forwardedClass"
    :severity="severity"
    :text="text"
    :outlined="outlined"
    :rounded="icon"
    :size="small ? 'small' : undefined"
    :loading="loading"
    :disabled="disabled || loading"
    :aria-busy="loading ? 'true' : undefined"
  >
    <slot />
  </PrimeButton>
</template>

<script setup lang="ts">
import { computed, useAttrs } from 'vue'
import PrimeButton from 'primevue/button'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md'
  icon?: boolean
  loading?: boolean
  disabled?: boolean
}>(), {
  variant: 'primary',
  size: undefined,
  icon: false,
  loading: false,
  disabled: false,
})

const attrs = useAttrs()
const forwardedClass = computed(() => attrs.class)
const forwardedAttrs = computed(() => {
  const { class: _class, ...rest } = attrs
  return rest
})
const severity = computed(() => props.variant === 'danger' ? 'danger' : props.variant === 'secondary' || props.variant === 'ghost' ? 'secondary' : undefined)
const text = computed(() => props.variant === 'ghost')
const outlined = computed(() => props.variant === 'secondary')
const small = computed(() => props.size === 'sm')
const icon = computed(() => props.icon)
const loading = computed(() => props.loading)
const disabled = computed(() => props.disabled)
</script>
