<template>
  <div class="appearance-switch" :class="{ 'appearance-switch-floating': floating }">
    <DropdownMenuRoot>
      <DropdownMenuTrigger as-child>
        <UiButton variant="ghost" icon :aria-label="`外观模式：${currentLabel}`" :title="`外观模式：${currentLabel}`">
          <UiIcon :name="appearance.preference.value === 'system' ? 'monitor' : appearance.resolved.value === 'dark' ? 'moon' : 'sun'" />
        </UiButton>
      </DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent class="ui-dropdown-content appearance-menu" align="end" :side-offset="6">
          <DropdownMenuLabel class="appearance-menu-label">外观模式</DropdownMenuLabel>
          <DropdownMenuRadioGroup :model-value="appearance.preference.value" @update:model-value="change">
            <DropdownMenuRadioItem v-for="option in options" :key="option.value" :value="option.value" class="ui-dropdown-item appearance-option">
              <UiIcon :name="option.icon" /><span>{{ option.label }}</span>
              <DropdownMenuItemIndicator class="appearance-check"><UiIcon name="check" /></DropdownMenuItemIndicator>
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { DropdownMenuContent, DropdownMenuItemIndicator, DropdownMenuLabel, DropdownMenuPortal, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import { useAppearance, type AppearancePreference } from '../theme/appearance'
defineProps<{ floating?: boolean }>()
const appearance = useAppearance()
const options = [
  { value: 'light', label: '浅色', icon: 'sun' },
  { value: 'dark', label: '深色', icon: 'moon' },
  { value: 'system', label: '跟随系统', icon: 'monitor' },
] as const
const currentLabel = computed(() => options.find(option => option.value === appearance.preference.value)!.label)
function change(value: unknown) {
  if (options.some(option => option.value === value)) appearance.setPreference(value as AppearancePreference)
}
</script>

<style scoped>
.appearance-switch { display: flex; align-items: center; flex: none; }
.appearance-switch-floating { position: fixed; z-index: 50; top: 16px; right: 20px; padding: 2px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--card); }
.appearance-menu { min-width: 160px; }
.appearance-menu-label { padding: 7px 10px; color: var(--muted-foreground); font-size: 11px; }
.appearance-option { display: flex; align-items: center; gap: 9px; }
.appearance-check { display: flex; margin-left: auto; color: var(--primary); }
@media (max-width: 560px) { .appearance-switch-floating { top: 12px; right: 12px; } }
</style>
