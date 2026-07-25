<template>
  <UiButton
    class="page-refresh-button"
    variant="secondary"
    icon
    type="button"
    :loading="loading"
    :disabled="disabled"
    :aria-label="label"
    :title="label"
    @click="$emit('click')"
  >
    <UiIcon name="refresh" />
  </UiButton>
</template>

<script setup lang="ts">
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  label?: string
  loading?: boolean
  disabled?: boolean
}>(), {
  label: '刷新',
  loading: false,
  disabled: false,
})

defineEmits<{ click: [] }>()
</script>

<style scoped>
.page-refresh-button.p-button {
  width: 38px;
  min-width: 38px;
  height: 38px;
  padding: 0;
}
</style>
