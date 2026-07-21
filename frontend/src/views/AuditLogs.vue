<template>
  <section>
    <PageHeader title="审计日志" description="追踪安全操作与业务状态变化。密码、凭证和敏感配置不会写入审计详情。" eyebrow="Audit Trail">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />刷新</button></template>
    </PageHeader>

    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <article class="panel">
      <header class="panel-header"><div><h2>事件记录</h2><p>支持按执行者、动作和目标精确筛选。</p></div><span class="result-count">{{ total }} 条事件</span></header>
      <form class="panel-body audit-filters" @submit.prevent="applyFilters">
        <label class="field"><span>执行者</span><input v-model.trim="actor" placeholder="例如：admin@local.com" /></label>
        <label class="field"><span>动作</span><input v-model.trim="action" placeholder="例如：order.pay" /></label>
        <label class="field"><span>目标</span><input v-model.trim="target" placeholder="例如：order:12" /></label>
        <button class="button" type="submit" :disabled="loading"><UiIcon name="search" />查询</button>
        <button v-if="hasFilters" class="button button-ghost" type="button" @click="clearFilters">清除筛选</button>
      </form>

      <div v-if="items.length" class="table-shell">
        <table class="data-table audit-table">
          <thead><tr><th>时间</th><th>执行者</th><th>动作</th><th>目标</th><th>详情</th><th>ID</th></tr></thead>
          <tbody><tr v-for="item in items" :key="item.id"><td class="time-cell">{{ formatDateTime(item.created_at) }}</td><td><div class="actor-cell"><span>{{ actorInitial(item.actor) }}</span><strong>{{ item.actor || 'system' }}</strong></div></td><td><StatusBadge tone="info">{{ actionLabel(item.action) }}</StatusBadge></td><td><code>{{ item.target || '—' }}</code></td><td class="detail-cell">{{ item.detail || '—' }}</td><td class="mono">#{{ item.id }}</td></tr></tbody>
        </table>
      </div>
      <EmptyState v-else icon="audit" title="没有匹配事件" description="当前筛选条件下没有审计记录。" />

      <footer class="pagination"><span>第 {{ total ? offset + 1 : 0 }}–{{ Math.min(offset + items.length, total) }} 条，共 {{ total }} 条</span><div><button class="button button-secondary button-sm" type="button" :disabled="loading || offset === 0" @click="previous">上一页</button><button class="button button-secondary button-sm" type="button" :disabled="loading || offset + limit >= total" @click="next">下一页</button></div></footer>
    </article>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchAuditLogs } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatDateTime } from '../utils/format'

const items = ref<any[]>([])
const total = ref(0)
const offset = ref(0)
const limit = 50
const actor = ref('')
const action = ref('')
const target = ref('')
const loading = ref(false)
const error = ref('')
const hasFilters = computed(() => Boolean(actor.value || action.value || target.value))

function actorInitial(value: string) { return (value || 'S').slice(0, 1).toUpperCase() }
function actionLabel(value: string) { return value?.replace(/[._-]+/g, ' ') || '未知动作' }
async function load() { loading.value = true; error.value = ''; try { const result = await fetchAuditLogs({ actor: actor.value || undefined, action: action.value || undefined, target: target.value || undefined, offset: offset.value, limit }); items.value = result.items || []; total.value = result.total || 0 } catch (e: any) { error.value = e?.response?.data?.message || '审计日志加载失败。' } finally { loading.value = false } }
async function applyFilters() { offset.value = 0; await load() }
async function clearFilters() { actor.value = ''; action.value = ''; target.value = ''; offset.value = 0; await load() }
async function previous() { offset.value = Math.max(0, offset.value - limit); await load() }
async function next() { offset.value += limit; await load() }
onMounted(load)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.result-count { color: var(--muted); font-size: 12px; }.audit-filters { display: grid; grid-template-columns: repeat(3, minmax(170px, 1fr)) auto auto; align-items: end; gap: 10px; border-bottom: 1px solid var(--line); }.audit-table { min-width: 960px; }.time-cell { width: 150px; white-space: nowrap; }.actor-cell { display: flex; align-items: center; gap: 8px; }.actor-cell span { width: 27px; height: 27px; display: grid; place-items: center; border-radius: 50%; color: #175cd3; background: var(--info-soft); font-size: 10px; font-weight: 750; }.actor-cell strong { font-size: 11px; }.audit-table code { color: #6941c6; background: #f4f3ff; padding: 3px 6px; border-radius: 5px; }.detail-cell { max-width: 440px; color: var(--muted) !important; white-space: normal; line-height: 1.5; }.pagination { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 14px 18px; border-top: 1px solid var(--line); color: var(--muted); font-size: 11px; }.pagination div { display: flex; gap: 7px; }
@media (max-width: 980px) { .audit-filters { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px) { .audit-filters { grid-template-columns: 1fr; }.pagination { align-items: stretch; flex-direction: column; }.pagination div > button { flex: 1; } }
</style>
