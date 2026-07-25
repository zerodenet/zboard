<template>
  <div class="template-output-options" role="radiogroup" aria-label="订阅输出方式">
    <UiButton
      v-for="option in subscriptionTemplateOutputOptions"
      :key="option.value"
      variant="secondary"
      type="button"
      role="radio"
      class="template-output-option"
      :class="{ 'template-output-option-selected': model === option.value }"
      :aria-checked="model === option.value ? 'true' : 'false'"
      @click="model = option.value"
    >
      <span class="template-output-icon"><UiIcon :name="option.icon" /></span>
      <span class="template-output-copy">
        <strong>{{ option.label }}</strong>
        <small>{{ option.description }}</small>
      </span>
      <UiIcon v-if="model === option.value" name="check" class="template-output-check" />
    </UiButton>
  </div>
</template>

<script setup lang="ts">
import type { SubscriptionRenderer } from '../api/client'
import { subscriptionTemplateOutputOptions } from '../utils/subscriptionTemplateEditor'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const model = defineModel<SubscriptionRenderer>({ required: true })
</script>

<style scoped>
.template-output-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.template-output-option {
  min-width: 0;
  min-height: 72px;
  justify-content: flex-start;
  padding: 12px;
  border-color: var(--line);
  background: var(--surface);
  color: var(--text-body);
  text-align: left;
}

.template-output-option:hover {
  border-color: var(--line-strong);
  background: var(--surface-soft);
}

.template-output-option-selected {
  border-color: var(--primary);
  background: var(--primary-soft);
  box-shadow: 0 0 0 1px var(--primary);
}

.template-output-icon {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 8px;
  color: var(--primary);
  background: var(--primary-soft);
}

.template-output-copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
}

.template-output-copy strong {
  color: var(--text-strong);
  font-size: 12px;
}

.template-output-copy small {
  color: var(--muted);
  font-size: 10px;
  font-weight: 500;
  line-height: 1.45;
  white-space: normal;
}

.template-output-check {
  align-self: flex-start;
  color: var(--primary);
}

@media (max-width: 700px) {
  .template-output-options {
    grid-template-columns: 1fr;
  }
}
</style>
