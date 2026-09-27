<template>
  <div v-if="contributions.length" class="plugin-slot" :class="{ 'plugin-slot--pending': optional && !ready }" :data-plugin-slot="name">
    <PluginFrame
      v-for="contribution in contributions"
      :key="`${contribution.plugin_id}:${contribution.slot.id}:${contribution.generation}:${targetUserId || 0}`"
      :plugin-id="contribution.plugin_id"
      :slot="contribution.slot"
      :surface="surface"
      :target-user-id="targetUserId"
      :title="contribution.slot.title"
      :optional="optional"
      @ready="ready = true"
    />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchPluginSlots, type CatalogSlot, type Surface } from '../api/plugins'
import PluginFrame from './PluginFrame.vue'

const props = defineProps<{ name: string; surface: Surface; targetUserId?: number; optional?: boolean }>()
const contributions = ref<CatalogSlot[]>([])
const ready = ref(false)
let controller = new AbortController()

async function load() {
  ready.value = false
  controller.abort()
  controller = new AbortController()
  try {
    contributions.value = await fetchPluginSlots(props.surface, props.name, controller.signal)
  } catch {
    contributions.value = []
  }
}

watch(() => [props.name, props.surface, props.targetUserId], load)
onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>

<style scoped>
.plugin-slot { display: grid; gap: 12px; min-width: 0; }
.plugin-slot--pending { display: none; }
.plugin-slot:empty { display: none; }
</style>
