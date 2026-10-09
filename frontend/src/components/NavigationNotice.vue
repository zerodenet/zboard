<template>
  <div class="navigation-notice" role="status">
    <UiIcon name="alert" /><p>{{ message }} 当前内容已保留。</p>
    <UiButton variant="ghost" size="sm" :loading="loading" @click="$emit('retry')">重试连接</UiButton>
  </div>
</template>
<script setup lang="ts">
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
defineProps<{ message: string; loading: boolean }>()
defineEmits<{ retry: [] }>()
</script>
<style scoped>
.navigation-notice { display: flex; align-items: center; gap: 8px; margin: 12px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 8px; color: var(--muted-foreground); background: var(--card); font-size: 12px; }
p { flex: 1; margin: 0; }
.ui-icon { flex: none; }
@media (max-width: 560px) { .navigation-notice { flex-wrap: wrap; } }
</style>
