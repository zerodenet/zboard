<template>
  <div class="entry-members">
    <EndpointMultiLookup :model-value="modelValue" service-kind="forward" placeholder="搜索前置入口名称或地址" @update:model-value="$emit('update:modelValue', $event)" />
    <small>只分配前置入口也会生成父协议认证凭据；直连地址只有单独选择后才会下发。协议参数与流量倍率沿用父协议。</small>
  </div>
</template>

<script setup lang="ts">
import EndpointMultiLookup from './EndpointMultiLookup.vue'
defineProps<{ modelValue: number[] }>()
defineEmits<{ 'update:modelValue': [number[]] }>()
</script>

<style scoped>
.entry-members{display:grid;gap:10px;margin-top:8px}.entry-members>small{color:var(--muted);font-size:11px}
</style>
