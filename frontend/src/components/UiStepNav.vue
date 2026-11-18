<template>
  <nav class="ui-step-nav" :aria-label="label">
    <ol class="ui-step-list" :style="{ gridTemplateColumns: `repeat(${steps.length}, minmax(0, 1fr))` }">
      <li v-for="step in steps" :key="step.id">
        <UiButton
          type="button"
          variant="ghost"
          class="ui-step-button"
          :class="{ 'is-active': current === step.id, 'is-complete': current > step.id }"
          :disabled="step.id > maxStep"
          :aria-current="current === step.id ? 'step' : undefined"
          @click="$emit('select', step.id)"
        >
          <span class="ui-step-index">
            <UiIcon v-if="current > step.id" name="check" />
            <span v-else>{{ step.id }}</span>
          </span>
          <span class="ui-step-title">{{ step.title }}</span>
        </UiButton>
      </li>
    </ol>
  </nav>
</template>

<script setup lang="ts">
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  steps: Array<{ id: number; title: string }>
  current: number
  maxStep: number
  label?: string
}>(), {
  label: '流程步骤',
})

defineEmits<{ select: [step: number] }>()
</script>

<style scoped>
.ui-step-list {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.ui-step-list li {
  min-width: 0;
}

:deep(.ui-step-button.ui-button) {
  width: 100%;
  min-width: 0;
  min-height: var(--control-height);
  justify-content: center;
  gap: 6px;
  padding-inline: 6px;
  border-bottom: 2px solid var(--border);
  border-radius: 0;
  color: var(--muted-foreground);
  white-space: normal;
}

:deep(.ui-step-button.ui-button.is-active) {
  border-bottom-color: var(--foreground);
  color: var(--foreground);
  font-weight: 600;
}

.ui-step-index {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  min-width: 12px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.ui-step-index .ui-icon {
  width: 12px;
  height: 12px;
}

.ui-step-title {
  min-width: 0;
  font-size: 12px;
}

@media (pointer: coarse) {
  :deep(.ui-step-button.ui-button) { min-height: 40px; }
}
</style>
