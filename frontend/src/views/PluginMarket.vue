<template>
  <div class="standard-page plugins-page plugins-list-page">
    <PageHeader title="添加插件" :show-section-navigation="false"><template #actions><PageRefreshButton label="刷新插件市场" :loading="loading" @click="load" /><UiButton as-child variant="secondary"><RouterLink to="/admin/plugins">管理插件</RouterLink></UiButton></template></PageHeader>
    <AdminPageNavigation />
    <p v-if="error" role="alert" class="plugins-error">
      {{ error }}
    </p>
    <div class="plugins-toolbar">
      <WorkbenchFilterBar :active="Boolean(query || surface)" :active-count="Number(Boolean(query)) + Number(Boolean(surface))" @clear="clearFilters">
        <WorkbenchFilterInput v-model="query" label="搜索" field-label="搜索市场插件" placeholder="名称或功能" />
        <WorkbenchFilterSelect v-model="surface" label="页面位置" :options="surfaceOptions" />
      </WorkbenchFilterBar>
      <span>{{ loaded ? `${filtered.length} 个插件` : '—' }}</span>
    </div>
    <div v-if="market.notice || market.generated_at" class="plugin-market-context">
      <p v-if="market.notice" role="status">{{ market.notice }}</p>
      <small v-if="market.generated_at">目录更新于 {{ formatDateTime(market.generated_at) }}</small>
    </div>
    <p v-if="loading && !loaded">正在加载市场目录…</p>
    <section v-else-if="loaded && !market.configured && !error" class="plugins-empty">
      <h2>尚未配置插件市场源</h2>
      <p>部署者配置可信目录地址与发布者公钥后，即可在这里浏览和安装。</p>
      <RouterLink to="/admin/plugins">已有插件包？前往离线导入</RouterLink>
    </section>
    <section v-else-if="loaded && !filtered.length && !error" class="plugins-empty">
      <h2>暂无匹配的插件</h2>
      <p>更换关键词，或稍后刷新目录。</p>
    </section>
    <div class="plugin-grid">
      <article v-for="entry in filtered" :key="entry.id" class="plugin-card">
        <div class="plugin-card-top">
          <div class="plugin-monogram">{{ entry.name.slice(0, 1) }}</div>
          <span class="plugin-status">{{ market.kind === 'registry' ? '官方收录' : '签名目录' }}</span>
        </div>
        <h2><RouterLink v-overflow-title="entry.name" class="entity-title-link" :to="`/admin/plugin-market/${encodeURIComponent(entry.id)}`">{{ entry.name }}</RouterLink></h2>
        <p class="plugin-id">{{ entry.id }}<template v-if="entry.version"> · v{{ entry.version }}</template></p>
        <p v-if="entry.description"><TableText :value="entry.description" /></p>
        <div class="plugin-tags">
          <span v-for="s in entry.surfaces" :key="s">{{
            surfaceLabel(s)
          }}</span>
        </div>
        <dl>
          <div>
            <dt>发布者</dt>
            <dd>{{ entry.publisher }}</dd>
          </div>
        </dl>
      </article>
    </div>
  </div>
</template>
<script setup lang="ts">
import PageHeader from '../components/PageHeader.vue'
import TableText from '../components/TableText.vue'
import { vOverflowTitle } from '../directives/overflowTitle'
import { computed, onMounted } from "vue";
import { useRoute, useRouter } from 'vue-router'
import { formatDateTime } from "../utils/format";
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import AdminPageNavigation from '../components/AdminPageNavigation.vue'
import UiButton from '../components/UiButton.vue'
import { useRemoteResource } from "../composables/useRemoteResource";
import {
  fetchPluginMarket,
  surfaceLabel,
  type Market,
} from "../api/plugins";
import "../styles/plugins.css";
const {
  data: market,
  loading,
  loaded,
  error,
  load,
} = useRemoteResource<Market>({
  initial: () => ({ configured: false, entries: [], expires_at: "" }),
  fetch: ({ signal }) => fetchPluginMarket(signal),
  errorMessage: "市场暂不可用，离线导入仍可使用。",
});
const surfaceOptions = [{ label: '所有页面位置', value: '' }, { label: '公开前台', value: 'public' }, { label: '用户前台', value: 'account' }, { label: '管理后台', value: 'admin' }]
const route = useRoute(), router = useRouter()
const query = computed({ get: () => String(route.query.q || ''), set: value => { void router.replace({ query: { ...route.query, q: value || undefined } }) } })
const surface = computed({ get: () => String(route.query.surface || ''), set: value => { void router.replace({ query: { ...route.query, surface: value || undefined } }) } })
function clearFilters() { void router.replace({ query: { ...route.query, q: undefined, surface: undefined } }) }
const filtered = computed(() =>
  market.value.entries.filter(
    (e) =>
      `${e.id} ${e.name} ${e.description}`
        .toLowerCase()
        .includes(query.value.toLowerCase()) &&
      (!surface.value || e.surfaces.includes(surface.value as any)),
  ),
);
onMounted(load);
</script>
