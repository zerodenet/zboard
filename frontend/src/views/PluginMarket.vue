<template>
  <div class="plugins-page">
    <header class="plugins-heading">
      <div>
        <p class="eyebrow">扩展中心</p>
        <h1>插件市场</h1>
        <p>发现前台与后台扩展，查看插件来源和发行状态，安装前验证签名和包内容。</p>
      </div>
      <RouterLink class="button button-secondary" to="/admin/plugins"
        >管理插件 / 离线导入</RouterLink
      >
    </header>
    <p v-if="error" role="alert" class="plugins-error">
      {{ error }}
    </p>
    <div class="plugins-toolbar">
      <UiInput
        v-model="query"
        placeholder="搜索名称或功能"
        aria-label="搜索市场插件"
      /><UiSelect v-model="surface" aria-label="页面位置" :options="surfaceOptions" /><button :disabled="loading" @click="load">刷新市场</button>
    </div>
    <p v-if="market.kind === 'registry'" class="plugin-market-notice">这里展示经 ZeroDeNet 准入的插件。版本由开发者仓库维护，进入详情可选择正式版、RC 或 Dev，在线安装前由 ZBoard 校验签名、兼容性和能力边界。</p>
    <p v-if="loading">正在加载市场目录…</p>
    <section v-else-if="!market.configured && !error" class="plugins-empty">
      <h2>尚未配置插件市场源</h2>
      <p>部署者配置可信目录地址与发布者公钥后，即可在这里浏览和安装。</p>
      <RouterLink to="/admin/plugins">已有插件包？前往离线导入</RouterLink>
    </section>
    <section v-else-if="!filtered.length && !error" class="plugins-empty">
      <h2>暂无匹配的插件</h2>
      <p>更换关键词，或稍后刷新目录。</p>
    </section>
    <div class="plugin-grid">
      <article v-for="entry in filtered" :key="entry.id" class="plugin-card">
        <div class="plugin-card-top">
          <div class="plugin-monogram">{{ entry.name.slice(0, 1) }}</div>
          <span class="plugin-status">{{ market.kind === 'registry' ? '官方收录' : '签名目录' }}</span>
        </div>
        <h2>{{ entry.name }}</h2>
        <p class="plugin-id">{{ entry.id }}<template v-if="entry.version"> · v{{ entry.version }}</template></p>
        <p>{{ entry.description }}</p>
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
        <RouterLink class="button button-secondary" :to="`/admin/plugin-market/${encodeURIComponent(entry.id)}`">详情 / 下载 / 安装</RouterLink>
      </article>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
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
  error,
  load,
} = useRemoteResource<Market>({
  initial: () => ({ configured: false, entries: [], expires_at: "" }),
  fetch: ({ signal }) => fetchPluginMarket(signal),
  errorMessage: "市场暂不可用，离线导入仍可使用。",
});
const surfaceOptions = [{ label: '所有页面位置', value: '' }, { label: '公开前台', value: 'public' }, { label: '用户前台', value: 'account' }, { label: '管理后台', value: 'admin' }]
const query = ref(""),
  surface = ref("");
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
