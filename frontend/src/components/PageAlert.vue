<template>
  <div
    class="page-alert"
    :class="`page-alert-${tone}`"
    :role="tone === 'danger' ? 'alert' : 'status'"
    aria-atomic="true"
  >
    <UiIcon :name="icon" />
    <div>
      <strong v-if="title">{{ title }}</strong>
      <p><slot /></p>
      <div v-if="$slots.actions" class="page-alert-actions"><slot name="actions" /></div>
    </div>
    <UiButton v-if="dismissible" variant="ghost" icon class="icon-button" type="button" aria-label="关闭提示" @click="$emit('dismiss')"><UiIcon name="close" /></UiButton>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{
  title?: string
  tone?: 'info' | 'success' | 'warning' | 'danger'
  dismissible?: boolean
}>(), { title: '', tone: 'info', dismissible: false })

defineEmits<{ dismiss: [] }>()
const icon = computed(() => props.tone === 'success' ? 'check' : props.tone === 'info' ? 'info' : 'alert')
</script>

<style scoped>
.page-alert-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}
</style>
