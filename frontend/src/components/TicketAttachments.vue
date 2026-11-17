<template>
  <div v-if="items?.length" class="ticket-attachments">
    <template v-for="item in items" :key="item.id || item.file_id || item.url">
      <UiButton v-if="item.file_id" type="button" variant="secondary" size="sm" :disabled="busy === item.file_id" @click="download(item)">下载 {{ item.name }}<small v-if="item.size"> · {{ formatBytes(item.size) }}</small></UiButton>
      <a v-else :href="item.url" target="_blank" rel="noopener noreferrer">{{ item.name }} ↗</a>
    </template>
    <p v-if="error" class="field-error" role="alert">{{ error }}</p>
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { downloadFile, type TicketAttachment } from '../api/files'
import { formatBytes } from '../utils/format'
import UiButton from './UiButton.vue'
defineProps<{ items?: TicketAttachment[] }>()
const busy = ref(''), error = ref('')
async function download(item: TicketAttachment) {
  if (!item.file_id) return
  busy.value = item.file_id; error.value = ''
  try { await downloadFile(item.file_id, item.name) }
  catch { error.value = '附件下载失败，请重试。' }
  finally { busy.value = '' }
}
</script>
<style scoped>
.ticket-attachments{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin-top:10px}.ticket-attachments a{font-size:12px;color:var(--primary);overflow-wrap:anywhere}
</style>
