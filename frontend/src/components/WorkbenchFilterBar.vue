<template>
  <div class="workbench-stripe-filters" role="group" :aria-label="label">
    <slot />
    <UiButton
      v-if="active"
      class="workbench-filter-clear-all"
      variant="ghost"
      size="sm"
      type="button"
      :disabled="loading"
      @click="$emit('clear')"
    >
      清除全部
    </UiButton>
  </div>
</template>

<script setup lang="ts">
import UiButton from './UiButton.vue'

withDefaults(defineProps<{
  active?: boolean
  loading?: boolean
  label?: string
}>(), {
  active: false,
  loading: false,
  label: '列表筛选',
})

defineEmits<{ clear: [] }>()
</script>
