<template>
  <PrimeAutoComplete
    v-model="model"
    v-bind="attrs"
    class="ui-autocomplete"
    fluid
  >
    <template #option="slotProps">
      <slot name="option" v-bind="slotProps" />
    </template>
    <template #dropdownicon>
      <UiIcon name="chevron" />
    </template>
  </PrimeAutoComplete>
</template>

<script setup lang="ts">
import { useAttrs } from 'vue'
import PrimeAutoComplete from 'primevue/autocomplete'
import UiIcon from './UiIcon.vue'

defineOptions({ inheritAttrs: false })

const attrs = useAttrs()
const model = defineModel<any>()
</script>

<style scoped>
.ui-autocomplete {
  width: 100%;
  min-width: 0;
  height: var(--control-height);
  display: flex;
  overflow: hidden;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: var(--surface);
  transition: border-color .15s ease, box-shadow .15s ease;
}

.ui-autocomplete:focus-within {
  border-color: var(--focus-border);
  box-shadow: 0 0 0 3px var(--focus-ring);
}

.ui-autocomplete:has(.p-autocomplete-input[aria-invalid='true']) {
  border-color: var(--danger);
}

.ui-autocomplete.p-disabled {
  color: var(--muted);
  background: var(--surface-muted);
}

.ui-autocomplete :deep(.p-autocomplete-input) {
  width: 100%;
  min-width: 0;
  min-height: calc(var(--control-height) - 2px);
  padding-inline: 12px;
  border: 0 !important;
  border-radius: 0 !important;
  background: transparent;
  box-shadow: none !important;
}

.ui-autocomplete :deep(.p-autocomplete-dropdown) {
  width: var(--control-height);
  min-width: var(--control-height);
  min-height: calc(var(--control-height) - 2px);
  margin: 0;
  padding: 0;
  border: 0 !important;
  border-left: 1px solid var(--line) !important;
  border-radius: 0 !important;
  color: var(--muted);
  background: var(--surface-soft);
  box-shadow: none !important;
}

.ui-autocomplete :deep(.p-autocomplete-dropdown:hover) {
  color: var(--primary);
  background: var(--primary-soft);
}

.ui-autocomplete :deep(.p-autocomplete-dropdown:focus-visible) {
  outline: 2px solid var(--focus-border);
  outline-offset: -3px;
}

.ui-autocomplete :deep(.p-autocomplete-dropdown .ui-icon) {
  width: 14px;
  height: 14px;
}

.ui-autocomplete :deep(.p-autocomplete-loader) {
  right: calc(var(--control-height) + 10px);
}
</style>
