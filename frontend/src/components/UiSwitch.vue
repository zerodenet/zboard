<template>
  <SwitchRoot v-bind="$attrs" v-model="model" class="ui-switch" :disabled="disabled">
    <SwitchThumb class="ui-switch-thumb" />
  </SwitchRoot>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from 'reka-ui'
defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{ disabled?: boolean }>(), { disabled: false })
const model = defineModel<boolean>({ default: false })
</script>
