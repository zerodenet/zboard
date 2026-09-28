<template>
  <section v-if="loading" class="navigation-page-state" role="status">正在加载页面…</section>
  <section v-else-if="error" class="navigation-page-state" role="alert">
    <h1>暂时无法加载页面</h1>
    <p>请重试，或稍后再访问。</p>
    <UiButton variant="secondary" @click="$emit('retry')">重试</UiButton>
  </section>
  <section v-else-if="!available" class="navigation-page-state" role="status">
    <h1>页面不可用</h1>
    <p>此页面已隐藏或当前无法访问。</p>
    <UiButton variant="secondary" @click="$emit('retry')">重新检查</UiButton>
  </section>
  <slot v-else />
</template>
<script setup lang="ts">
import UiButton from './UiButton.vue'
defineProps<{ available: boolean; loading: boolean; error: string }>()
defineEmits<{ retry: [] }>()
</script>
<style scoped>
.navigation-page-state { padding: 48px 24px; color: var(--muted-foreground); }
.navigation-page-state h1 { margin: 0 0 12px; color: var(--foreground); font-size: 20px; }
.navigation-page-state p { margin-bottom: 20px; }
</style>
