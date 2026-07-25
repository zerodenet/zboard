<template>
  <nav class="subscription-template-section-nav" aria-label="订阅模板功能">
    <RouterLink
      v-for="item in items"
      :key="item.value"
      class="section-link"
      :class="{ 'is-active': item.value === section }"
      :to="item.to"
      :aria-current="item.value === section ? 'page' : undefined"
    >
      {{ item.label }}
    </RouterLink>
  </nav>
</template>

<script setup lang="ts">
type SubscriptionTemplateSection = 'templates' | 'rule-sets'

defineProps<{ section: SubscriptionTemplateSection }>()
const items = [
  { value: 'templates', label: '模板', to: '/admin/subscription-templates' },
  { value: 'rule-sets', label: '规则集', to: '/admin/subscription-templates/rule-sets' },
]
</script>

<style scoped>
.subscription-template-section-nav {
  min-width: 0;
  min-height: 36px;
  display: flex;
  align-items: flex-end;
  gap: 24px;
  padding: 0 2px;
  overflow-x: auto;
  border-bottom: 1px solid var(--line);
  scrollbar-width: none;
}

.subscription-template-section-nav::-webkit-scrollbar {
  display: none;
}

.section-link {
  position: relative;
  min-height: 36px;
  display: inline-flex;
  align-items: center;
  padding: 0 1px 9px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  line-height: 1;
  text-decoration: none;
  white-space: nowrap;
  transition: color 120ms ease;
}

.section-link::after {
  content: "";
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  border-radius: 2px 2px 0 0;
  background: var(--primary);
  opacity: 0;
  transform: scaleX(.65);
  transition: opacity 120ms ease, transform 120ms ease;
}

.section-link:hover,
.section-link.is-active {
  color: var(--text-strong);
}

.section-link.is-active {
  font-weight: 700;
}

.section-link.is-active::after {
  opacity: 1;
  transform: scaleX(1);
}

.section-link:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 3px;
  border-radius: 3px;
}
</style>
