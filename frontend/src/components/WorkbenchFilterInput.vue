<template>
  <WorkbenchFilterChip
    :label="label"
    :active="active"
    :value-label="displayValue"
    icon="search"
    @open="restoreDraft"
    @clear="clear"
  >
    <template #default="{ close }">
      <div class="workbench-filter-form" @keyup.enter="apply(close)">
        <slot name="field" :draft="draft" :set-draft="setDraft">
          <label class="workbench-filter-field"><span>{{ fieldLabel || label }}</span>
            <UiInput v-model="draft" v-bind="$attrs" :aria-label="fieldLabel || label" :placeholder="placeholder || label" />
          </label>
        </slot>
        <div class="workbench-filter-form-actions">
          <slot name="actions" :apply="() => apply(close)" :reset="resetDraft">
            <UiButton variant="secondary" size="sm" type="button" @click="resetDraft">重置</UiButton>
            <UiButton size="sm" type="button" @click="apply(close)"><UiIcon name="search" />搜索</UiButton>
          </slot>
        </div>
      </div>
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  label: string
  placeholder?: string
  valuePrefix?: string
  fieldLabel?: string
}>(), {
  placeholder: '',
  valuePrefix: '',
  fieldLabel: '',
})

const model = defineModel<string>({ default: '' })
const draft = ref(model.value)
const emit = defineEmits<{ apply: [] }>()
const active = computed(() => Boolean(model.value.trim()))
const displayValue = computed(() => active.value ? `${props.valuePrefix}${model.value.trim()}` : '')

watch(model, value => {
  draft.value = value
})

function resetDraft() {
  draft.value = ''
}

function restoreDraft() { draft.value = model.value }
function setDraft(value: string) { draft.value = value }

async function apply(close?: (restoreFocus?: boolean) => void) {
  model.value = draft.value.trim()
  await nextTick()
  emit('apply')
  close?.(true)
}

async function clear() {
  model.value = ''
  draft.value = ''
  await nextTick()
  emit('apply')
}
</script>
