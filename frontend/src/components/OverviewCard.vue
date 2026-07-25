<template>
  <component
    :is="interactive ? 'button' : 'article'"
    class="overview-card"
    :class="{ 'overview-card-interactive': interactive }"
    :data-tone="tone"
    :type="interactive ? 'button' : undefined"
    :aria-pressed="interactive ? selected : undefined"
    :disabled="interactive ? disabled || loading : undefined"
    @click="interactive && $emit('select')"
  >
    <span class="overview-card-icon metric-icon" aria-hidden="true"><UiIcon :name="icon" /></span>
    <span class="overview-card-copy">
      <span class="overview-card-heading">
        <span class="overview-card-label metric-label">{{ label }}</span>
        <strong class="overview-card-value metric-value">{{ loading ? '—' : value }}</strong>
      </span>
      <span class="overview-card-description" :title="description"><slot name="description">{{ description }}</slot></span>
    </span>
    <span v-if="$slots.badge" class="overview-card-badge"><slot name="badge" /></span>
  </component>
</template>

<script setup lang="ts">
import UiIcon from './UiIcon.vue'

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

withDefaults(defineProps<{
  label: string
  value: string | number
  icon: string
  description?: string
  tone?: Tone
  interactive?: boolean
  selected?: boolean
  loading?: boolean
  disabled?: boolean
}>(), {
  description: '',
  tone: 'neutral',
  interactive: false,
  selected: false,
  loading: false,
  disabled: false,
})

defineEmits<{ select: [] }>()
</script>

<style scoped>
.overview-card {
  min-width: 0;
  min-height: 108px;
  position: relative;
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  align-content: start;
  align-items: start;
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 10px;
  color: var(--text-body);
  background: var(--surface);
  box-shadow: 0 1px 2px color-mix(in srgb, var(--text) 5%, transparent);
  font: inherit;
  text-align: left;
}
.overview-card-icon {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  color: var(--text-secondary);
  background: var(--surface-neutral);
}
.overview-card[data-tone='info'] .overview-card-icon { color: var(--primary); background: var(--primary-soft); }
.overview-card[data-tone='success'] .overview-card-icon { color: var(--success); background: var(--success-soft); }
.overview-card[data-tone='warning'] .overview-card-icon { color: var(--warning); background: var(--warning-soft); }
.overview-card[data-tone='danger'] .overview-card-icon { color: var(--danger); background: var(--danger-soft); }
.overview-card-copy { min-width: 0; display: grid; gap: 3px; }
.overview-card-heading { min-width: 0; display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: baseline; gap: 8px; }
.overview-card-label { overflow: hidden; color: var(--text-secondary); font-size: 11px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.overview-card-value { color: var(--text-strong); font-size: 24px; font-weight: 720; letter-spacing: -.04em; font-variant-numeric: tabular-nums; }
.overview-card-description { overflow: hidden; color: var(--muted); font-size: 9px; font-weight: 500; line-height: 1.4; text-overflow: ellipsis; white-space: nowrap; }
.overview-card-badge { position: absolute; right: 12px; bottom: 12px; }
.overview-card-interactive {
  cursor: pointer;
  transition: border-color .15s ease, box-shadow .15s ease, transform .15s ease;
}
.overview-card-interactive:hover:not(:disabled) {
  border-color: var(--line-strong);
  box-shadow: 0 3px 10px color-mix(in srgb, var(--text) 8%, transparent);
  transform: translateY(-1px);
}
.overview-card-interactive:focus-visible {
  outline: 0;
  border-color: var(--focus-border);
  box-shadow: 0 0 0 3px var(--focus-ring);
}
.overview-card-interactive[aria-pressed='true'] {
  border-color: var(--primary);
  box-shadow: 0 0 0 1px var(--primary), 0 4px 14px color-mix(in srgb, var(--primary) 13%, transparent);
}
.overview-card-interactive:disabled { cursor: not-allowed; opacity: .58; }

@media (max-width: 560px) {
  .overview-card { min-height: 98px; }
}
</style>
