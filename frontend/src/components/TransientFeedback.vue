<template>
  <PageAlert v-if="error" class="transient-page-error" tone="danger" :title="errorTitle">{{ error }}</PageAlert>
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { notify } from '../utils/feedback'
import PageAlert from './PageAlert.vue'

const props = withDefaults(defineProps<{
  success?: string
  error?: string
  successTitle?: string
  errorTitle?: string
}>(), {
  success: '',
  error: '',
  successTitle: '操作已完成',
  errorTitle: '操作未完成'
})

watch(() => props.success, (message) => {
  if (message) notify(props.successTitle, message, 'success')
}, { immediate: true })

</script>
