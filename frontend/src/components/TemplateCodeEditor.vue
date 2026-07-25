<template>
  <div class="template-code-editor" :class="{ 'template-code-editor-invalid': Boolean($attrs['aria-invalid']) }">
    <div ref="gutter" class="template-code-gutter" aria-hidden="true">
      <span v-for="line in lineCount" :key="line" :class="{ invalid: line === errorLine }">{{ line }}</span>
    </div>
    <UiTextarea
      ref="editor"
      v-model="model"
      v-bind="$attrs"
      rows="20"
      spellcheck="false"
      @scroll="syncScroll"
      @keydown.tab.prevent="insertIndent"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import UiTextarea from './UiTextarea.vue'

defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{ errorLine?: number }>(), { errorLine: 0 })
const model = defineModel<string>({ default: '' })
const editor = ref<InstanceType<typeof UiTextarea> | null>(null)
const gutter = ref<HTMLElement | null>(null)
const lineCount = computed(() => Math.max(1, String(model.value || '').split('\n').length))

function syncScroll(event: Event) {
  if (gutter.value) gutter.value.scrollTop = (event.target as HTMLTextAreaElement).scrollTop
}

async function insertIndent(event: KeyboardEvent) {
  const textarea = event.target as HTMLTextAreaElement
  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  const value = String(model.value || '')
  model.value = `${value.slice(0, start)}  ${value.slice(end)}`
  await nextTick()
  textarea.setSelectionRange(start + 2, start + 2)
}

defineExpose({
  focus: () => (editor.value?.$el as HTMLTextAreaElement | undefined)?.focus(),
})
</script>

<style scoped>
.template-code-editor { height: 360px; min-width: 0; display: grid; grid-template-columns: 42px minmax(0, 1fr); overflow: hidden; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--surface); transition: border-color .15s ease, box-shadow .15s ease; }
.template-code-editor:focus-within { border-color: var(--focus-border); box-shadow: 0 0 0 3px var(--focus-ring); }
.template-code-editor-invalid { border-color: var(--danger); }
.template-code-gutter { height: 100%; overflow: hidden; padding: 10px 0; border-right: 1px solid var(--line); color: var(--subtle); background: var(--surface-soft); font-family: var(--font-mono); font-size: 11px; line-height: 1.6; text-align: right; }
.template-code-gutter span { display: block; padding-right: 9px; }
.template-code-gutter span.invalid { color: var(--danger); background: var(--danger-soft); font-weight: 750; }
:deep(.p-textarea) { height: 100%!important; min-height: 0; resize: none; padding: 10px 12px; border: 0; border-radius: 0; box-shadow: none!important; font-family: var(--font-mono); font-size: 11px; line-height: 1.6; tab-size: 2; }
@media(max-width:700px){.template-code-editor{height:320px;grid-template-columns:36px minmax(0,1fr)}.template-code-gutter span{padding-right:7px}}
</style>
