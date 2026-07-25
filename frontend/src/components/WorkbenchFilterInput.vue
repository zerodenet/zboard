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
        <UiInput
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
import UiInput from './UiInput.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  label: string
  placeholder?: string
  valuePrefix?: string
}>(), {
  placeholder: '',
  valuePrefix: '',
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
  draft.value = model.value
}

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
