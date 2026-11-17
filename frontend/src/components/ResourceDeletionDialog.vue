<template>
  <ModalDialog :open="!!target" :title="`删除${kind === 'protocol' ? '协议服务' : '前置入口'}：${target?.name || ''}`" :busy="busy" size="md" @close="close">
    <p>资源 #{{ target?.id }} · {{ target?.name }}</p>
    <p v-if="checking" role="status">正在检查删除依赖…</p>
    <PageAlert v-if="failure" tone="danger" title="删除未完成">{{ failure }}</PageAlert>
    <PageAlert v-if="check?.already_deleted" tone="info" title="该资源已删除">历史流量和账本保留，无需再次删除。</PageAlert>
    <div v-for="blocker in check?.blockers || []" :key="`${blocker.code}:${blocker.href}`" class="deletion-blocker">
      <PageAlert tone="danger" title="暂时无法删除">{{ blocker.message }} <RouterLink v-if="blocker.href" :to="blocker.href">查看并处理</RouterLink></PageAlert>
    </div>
    <div v-for="warning in check?.warnings || []" :key="`${warning.code}:${warning.href}`" class="deletion-blocker">
      <PageAlert tone="warning" title="删除影响">{{ warning.message }} <RouterLink v-if="warning.href" :to="warning.href">查看关联</RouterLink></PageAlert>
    </div>
    <ul v-if="check && !check.already_deleted" class="deletion-effects"><li v-for="effect in check.effects" :key="effect">{{ effect }}</li></ul>
    <p v-if="check && !check.already_deleted">删除无法在面板内撤销，请确认以上影响。</p>
    <template #footer>
      <UiButton variant="secondary" :disabled="busy" @click="close">取消</UiButton>
      <UiButton variant="secondary" :disabled="busy" :loading="checking" @click="inspect">重新检查</UiButton>
      <UiButton v-if="check?.already_deleted" @click="emit('deleted')">关闭并刷新列表</UiButton>
      <UiButton v-else variant="danger" :loading="busy" :disabled="checking || !check || !!check.blockers.length" @click="remove">确认删除</UiButton>
    </template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { checkResourceDeletion, deleteNetworkEntry, deleteProtocolEndpoint, type ResourceDeletionCheck } from '../api/client'
import ModalDialog from './ModalDialog.vue'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
const props = defineProps<{ target: { id: number; name: string } | null; kind: 'protocol' | 'entry' }>()
const emit = defineEmits<{ close: []; deleted: [] }>()
const check = ref<ResourceDeletionCheck | null>(null)
const checking = ref(false)
const busy = ref(false)
const failure = ref('')
let sequence = 0
function close() { if (!busy.value) emit('close') }
async function inspect() {
  const target = props.target
  if (!target || busy.value) return
  const current = ++sequence
  checking.value = true; check.value = null; failure.value = ''
  try { const result = await checkResourceDeletion(props.kind, target.id); if (current === sequence) check.value = result }
  catch (cause: any) { if (current === sequence) failure.value = cause?.response?.data?.message || '无法检查删除依赖，请重新检查。' }
  finally { if (current === sequence) checking.value = false }
}
async function remove() {
  if (!props.target || busy.value || checking.value || !check.value || check.value.blockers.length) return
  busy.value = true; failure.value = ''
  try {
    if (props.kind === 'protocol') await deleteProtocolEndpoint(props.target.id)
    else await deleteNetworkEntry(props.target.id)
    emit('deleted')
  } catch (cause: any) {
    failure.value = cause?.response?.data?.message || '删除未完成，请检查网络和任务结果后重试。'
    const updated = cause?.response?.data?.data
    if (updated?.id === props.target.id && Array.isArray(updated.blockers)) check.value = updated
  } finally { busy.value = false }
}
watch(() => [props.target?.id, props.kind], () => { sequence++; check.value = null; failure.value = ''; checking.value = false; if (props.target) void inspect() }, { immediate: true })
</script>
<style scoped>
.deletion-blocker { margin-block: 12px; }
.deletion-effects { padding-left: 20px; line-height: 1.7; }
p, li { overflow-wrap: anywhere; }
</style>
