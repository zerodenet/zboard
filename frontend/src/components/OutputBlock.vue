<template>
  <section class="output-block" :class="{ 'output-block-danger': tone === 'danger' }">
    <header>
      <span>{{ label }}</span>
      <UiButton variant="ghost" size="sm" type="button" :aria-label="`复制${label}`" @click="copy">
        <UiIcon :name="copied ? 'check' : 'copy'" />{{ copied ? '已复制' : '复制' }}
      </UiButton>
    </header>
    <pre>{{ displayValue }}</pre>
    <small v-if="truncated">内容过长，界面仅展示前 {{ maxLength.toLocaleString('zh-CN') }} 个字符；复制操作使用已清理的完整内容。</small>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { normalizeOutput, truncateOutput } from '../utils/output'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{ value?: string | null; label?: string; tone?: 'neutral' | 'danger'; maxLength?: number }>(), {
  value: '', label: '输出', tone: 'neutral', maxLength: 12000
})
const copied = ref(false)

const normalized = computed(() => normalizeOutput(props.value))
const truncated = computed(() => normalized.value.length > props.maxLength)
const displayValue = computed(() => truncateOutput(normalized.value, props.maxLength))

async function copy() {
  await navigator.clipboard.writeText(normalized.value)
  copied.value = true
  window.setTimeout(() => { copied.value = false }, 1600)
}
</script>

<style scoped>
.output-block { min-width: 0; margin-top: 10px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); }.output-block header { display: flex; align-items: center; justify-content: space-between; min-height: 36px; padding: 4px 8px 4px 12px; border-bottom: 1px solid var(--line); font-size: 10px; font-weight: 700; }.output-block pre { max-height: 280px; margin: 0; padding: 12px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; tab-size: 2; font-family: var(--font-mono); font-size: 10px; line-height: 1.55; }.output-block small { display: block; padding: 8px 12px; border-top: 1px solid var(--line); color: var(--muted); font-size: 9px; }.output-block-danger { border-color: color-mix(in srgb, var(--danger) 30%, var(--line)); }.output-block-danger header { color: var(--danger); background: var(--danger-soft); }
</style>
