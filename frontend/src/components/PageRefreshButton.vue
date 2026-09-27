<template>
  <Tooltip :text="label"><UiButton
    class="page-refresh-button"
    :variant="variant"
    icon
    type="button"
    :loading="loading"
    :disabled="disabled"
    :aria-label="label"
    :title="label"
    @click="$emit('click')"
  >
    <UiIcon name="refresh" />
  </UiButton></Tooltip>
</template>

<script setup lang="ts">
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import Tooltip from './ui/Tooltip.vue'

withDefaults(defineProps<{
  label?: string
  loading?: boolean
  disabled?: boolean
  variant?: 'secondary' | 'ghost'
}>(), {
  label: '刷新',
  loading: false,
  disabled: false,
  variant: 'ghost',
})

defineEmits<{ click: [] }>()
</script>

<style scoped>
.page-refresh-button.ui-button {
  width: 32px;
  min-width: 32px;
  height: 32px;
  padding: 0;
}
.page-refresh-button.ui-button-ghost { border-color: transparent; background: transparent; box-shadow: none; }
.page-refresh-button.ui-button-ghost:hover:not(:disabled) { color: var(--text-strong); background: transparent; }
</style>
