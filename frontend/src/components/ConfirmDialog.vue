<template>
  <ModalDialog :open="open" :title="title" :description="description" size="sm" :busy="busy" @close="$emit('close')">
    <div class="confirm-content" :data-tone="tone">
      <span><UiIcon :name="tone === 'danger' ? 'alert' : 'shield'" /></span>
      <p><slot>{{ message }}</slot></p>
    </div>
    <PageAlert v-if="error" tone="danger" title="无法完成操作">{{ error }}</PageAlert>
    <template #footer>
      <UiButton variant="secondary" type="button" :disabled="busy" @click="$emit('close')">取消</UiButton>
      <UiButton :variant="tone === 'danger' ? 'danger' : 'primary'" type="button" :loading="busy" @click="$emit('confirm')">
        {{ busy ? '处理中…' : confirmText }}
      </UiButton>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import ModalDialog from './ModalDialog.vue'
import PageAlert from './PageAlert.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{ open: boolean; title: string; description?: string; message?: string; error?: string; confirmText?: string; tone?: 'primary' | 'danger'; busy?: boolean }>(), {
  confirmText: '确认', tone: 'primary', busy: false, message: '', error: ''
})
defineEmits<{ close: []; confirm: [] }>()
</script>

<style scoped>
.confirm-content { display: grid; grid-template-columns: auto 1fr; align-items: flex-start; gap: 12px; }.confirm-content > span { width: 38px; height: 38px; display: grid; place-items: center; border-radius: 10px; color: var(--primary); background: var(--primary-soft); font-size: 19px; }.confirm-content[data-tone='danger'] > span { color: var(--danger); background: var(--danger-soft); }.confirm-content p { margin: 3px 0 0; color: var(--muted); font-size: 13px; line-height: 1.65; }
</style>
