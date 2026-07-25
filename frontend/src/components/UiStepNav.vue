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
          <span class="ui-step-copy">
            <strong>{{ step.title }}</strong>
            <small>{{ step.caption }}</small>
          </span>
        </UiButton>
      </li>
    </ol>
  </nav>
</template>

<script setup lang="ts">
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  steps: Array<{ id: number; title: string; caption: string }>
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

:deep(.ui-step-button.p-button) {
  width: 100%;
  min-height: 56px;
  justify-content: flex-start;
  gap: 10px;
  padding: 9px 12px;
  border: 1px solid var(--line) !important;
  border-radius: var(--radius-sm) !important;
  color: var(--muted) !important;
  background: var(--surface) !important;
  box-shadow: none !important;
  text-align: left;
}

:deep(.ui-step-button.p-button:hover:not(:disabled)) {
  border-color: var(--line-strong) !important;
  background: var(--surface-hover) !important;
}

:deep(.ui-step-button.p-button.is-active) {
  border-color: var(--primary-border) !important;
  color: var(--primary) !important;
  background: var(--primary-soft) !important;
  box-shadow: 0 0 0 1px var(--primary-border) !important;
}

:deep(.ui-step-button.p-button.is-complete) {
  color: var(--text-body) !important;
  background: var(--surface-soft) !important;
}

:deep(.ui-step-button.p-button:disabled) {
  opacity: 1;
}

.ui-step-index {
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  border-radius: 50%;
  color: var(--muted);
  background: var(--surface-neutral);
  font-size: 10px;
  font-weight: 750;
}

.is-active .ui-step-index,
.is-complete .ui-step-index {
  color: var(--text-inverse);
  background: var(--primary);
}

.ui-step-index .ui-icon {
  width: 12px;
  height: 12px;
}

.ui-step-copy {
  min-width: 0;
  display: grid;
  gap: 2px;
}

.ui-step-copy strong {
  font-size: 11px;
}

.ui-step-copy small {
  color: var(--muted);
  font-size: 9px;
}

@media (max-width: 680px) {
  .ui-step-list {
    gap: 6px;
  }

  :deep(.ui-step-button.p-button) {
    min-height: 44px;
    justify-content: center;
    padding: 8px 6px;
  }

  .ui-step-copy {
    display: none;
  }
}
</style>
