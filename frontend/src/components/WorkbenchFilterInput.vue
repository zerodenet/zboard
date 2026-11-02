<template>
  <form v-if="inline" class="workbench-inline-search" role="search" :aria-label="label" @submit.prevent="apply()">
    <UiInput v-model="draft" v-bind="$attrs" type="search" :aria-label="fieldLabel || label" :placeholder="placeholder || label" />
    <UiButton variant="secondary" size="sm" type="submit">搜索</UiButton>
  </form>
  <WorkbenchFilterChip v-else
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
  inline?: boolean
}>(), {
  placeholder: '',
  valuePrefix: '',
  fieldLabel: '',
  inline: false,
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

<style scoped>
.workbench-inline-search { display: flex; align-items: center; gap: 6px; flex: 0 1 320px; min-width: 0; }
.workbench-inline-search .ui-input { flex: 1; min-width: 0; }
</style>
