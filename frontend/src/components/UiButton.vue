<template>
  <Button
    type="button"
    v-bind="attrs"
    :variant="variant === 'primary' ? 'default' : variant === 'danger' ? 'destructive' : variant"
    :size="icon ? 'icon' : size === 'md' ? 'default' : size"
    :class="['ui-button', `ui-button-${variant === 'primary' ? 'default' : variant === 'danger' ? 'destructive' : variant}`, `ui-button-${size}`, { 'ui-button-icon': icon }]"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
  ><span v-if="loading" class="ui-button-spinner" aria-hidden="true" /><slot v-if="!loading || !icon" /></Button>
</template>

<script setup lang="ts">
import { useAttrs } from 'vue'
import { Button } from './ui/button'

defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{
  variant?: 'primary' | 'default' | 'secondary' | 'outline' | 'ghost' | 'danger' | 'destructive' | 'link'
  size?: 'sm' | 'md' | 'lg'
  icon?: boolean
  loading?: boolean
  disabled?: boolean
}>(), { variant: 'primary', size: 'md', icon: false, loading: false, disabled: false })
const attrs = useAttrs()
</script>
