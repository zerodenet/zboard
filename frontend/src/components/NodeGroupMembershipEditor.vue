<template>
  <div class="membership-editor">
    <div class="membership-add">
      <NodeGroupLookup v-model="candidateID" :disabled="!canAdd" placeholder="搜索并选择要关联的节点组" @select="addSelectedGroup" />
    </div>
    <p v-if="!canAdd" class="membership-note">协议服务停用时不能新增节点组关联；已有关联仍可移除。</p>
    <p v-else class="membership-note">选中后立即加入下方关联列表，保存协议服务时生效。</p>
    <div v-if="modelValue.length" class="membership-list">
      <article v-for="item in modelValue" :key="item.node_group_id">
        <span><strong>{{ item.name }}</strong><small>{{ item.code }} · 版本 {{ item.revision }}<template v-if="!item.is_enabled"> · 已停用</template></small></span>
        <UiButton variant="ghost" size="sm" icon type="button" :aria-label="`移除节点组 ${item.name}`" @click="remove(item.node_group_id)"><UiIcon name="close" /></UiButton>
      </article>
    </div>
    <p v-else class="membership-empty">尚未关联节点组。保存后，该协议服务不会出现在任何套餐交付边界中。</p>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import type { NodeGroupSummary, ProtocolEndpointNodeGroupMembership } from '../api/client'
import NodeGroupLookup from './NodeGroupLookup.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{ modelValue: ProtocolEndpointNodeGroupMembership[]; canAdd?: boolean }>(), { canAdd: true })
const emit = defineEmits<{ 'update:modelValue': [value: ProtocolEndpointNodeGroupMembership[]] }>()
const candidateID = ref(0)
const selectedIDs = computed(() => new Set(props.modelValue.map(item => item.node_group_id)))

async function addSelectedGroup(candidate: NodeGroupSummary | null) {
  if (!props.canAdd || !candidate || selectedIDs.value.has(candidate.id)) return
  const item: ProtocolEndpointNodeGroupMembership = {
    node_group_id: candidate.id,
    name: candidate.name,
    code: candidate.code,
    description: candidate.description || '',
    is_enabled: candidate.is_enabled,
    revision: candidate.revision,
    sort_order: 0,
  }
  emit('update:modelValue', [...props.modelValue, item])
  await nextTick()
  candidateID.value = 0
}
function remove(id: number) { emit('update:modelValue', props.modelValue.filter(item => item.node_group_id !== id)) }
</script>

<style scoped>
.membership-editor{display:grid;gap:10px}.membership-add{display:grid;grid-template-columns:minmax(0,1fr);align-items:start;gap:8px}.membership-list{display:grid;border:1px solid var(--line);border-radius:9px;background:var(--surface)}.membership-list article{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 12px}.membership-list article+article{border-top:1px solid var(--line)}.membership-list strong,.membership-list small{display:block}.membership-list strong{font-size:11px}.membership-list small,.membership-note,.membership-empty{margin:0;color:var(--muted);font-size:9px}.membership-empty{padding:10px;border:1px dashed var(--line-strong);border-radius:9px;background:var(--surface-soft)}@media(max-width:640px){.membership-add{grid-template-columns:1fr}}
</style>
