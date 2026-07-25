<template>
  <WorkbenchFilterChip
    :label="label"
    :active="active"
    :value-label="displayValue"
    @open="resetDraft"
    @clear="clear"
  >
    <template #default="{ close }">
      <div class="workbench-filter-form">
        <UiNumberInput
          v-model="draft"
          v-bind="$attrs"
          :aria-label="label"
          :placeholder="placeholder || label"
          @keyup.enter="apply(close)"
        />
        <UiButton size="sm" type="button" @click="apply(close)"><UiIcon name="check" />应用</UiButton>
      </div>
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiNumberInput from './UiNumberInput.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  label: string
  placeholder?: string
  valuePrefix?: string
  emptyValue?: number | null
}>(), {
  placeholder: '',
  valuePrefix: '',
  emptyValue: 0,
})

const model = defineModel<number | null>({ default: null })
const draft = ref<number | null>(model.value)
const emit = defineEmits<{ apply: [] }>()
const active = computed(() => !Object.is(model.value, props.emptyValue))
const displayValue = computed(() => active.value ? `${props.valuePrefix}${model.value}` : '')

watch(model, value => {
  draft.value = value
})

function resetDraft() {
  draft.value = model.value
}

async function apply(close?: (restoreFocus?: boolean) => void) {
  model.value = draft.value
  await nextTick()
  emit('apply')
  close?.(true)
}

async function clear() {
  model.value = props.emptyValue
  draft.value = props.emptyValue
  await nextTick()
  emit('apply')
}
</script>
