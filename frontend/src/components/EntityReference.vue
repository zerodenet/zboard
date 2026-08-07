<template>
  <div class="entity-reference" :class="{ compact, missing: resolved.missing }">
    <strong>{{ resolved.display_name }}</strong>
    <span v-if="meta">{{ meta }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fallbackEntityReference, type EntityKind, type EntityReference } from '../api/readModels'

const props = withDefaults(defineProps<{
  reference?: EntityReference | null
  fallbackId?: number
  fallbackKind?: EntityKind
  compact?: boolean
  showId?: boolean
}>(), {
  reference: null,
  fallbackId: 0,
  fallbackKind: 'entity',
  compact: false,
  showId: true,
})

const labels: Record<string, string> = {
  user: '用户',
  subscription: '订阅',
  node: '节点',
  protocol_endpoint: '端点',
  plan: '套餐',
  plan_sku: '规格',
  order: '订单',
}

const resolved = computed(() => props.reference || fallbackEntityReference(props.fallbackKind, props.fallbackId))
const meta = computed(() => {
  const parts: string[] = []
  if (resolved.value.secondary) parts.push(resolved.value.secondary)
  const id = resolved.value.id || props.fallbackId
  if (props.showId && id) parts.push(`${labels[resolved.value.kind] || labels[props.fallbackKind] || 'ID'} #${id}`)
  return parts.join(' · ')
})
</script>

<style scoped>
.entity-reference {
  min-width: 0;
  display: grid;
  gap: 3px;
}

.entity-reference strong {
  overflow: hidden;
  color: var(--text-strong);
  font-size: 11px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.entity-reference span {
  overflow: hidden;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.entity-reference.compact {
  gap: 1px;
}

.entity-reference.missing strong {
  color: var(--muted);
}
</style>
