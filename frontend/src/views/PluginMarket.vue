<template>
  <div class="plugins-page">
    <header class="plugins-heading">
      <div>
        <p class="eyebrow">扩展中心</p>
        <h1>插件市场</h1>
        <p>发现前台与后台扩展，安装前验证发布者签名和包内容。</p>
      </div>
      <RouterLink class="button button-secondary" to="/admin/plugins"
        >管理插件 / 离线导入</RouterLink
      >
    </header>
    <p v-if="error || actionError" role="alert" class="plugins-error">
      {{ actionError || error }}
    </p>
    <TransientFeedback :success="message" />
    <div class="plugins-toolbar">
      <UiInput
        v-model="query"
        placeholder="搜索名称或功能"
        aria-label="搜索市场插件"
      /><UiSelect v-model="surface" aria-label="页面位置" :options="surfaceOptions" /><button :disabled="loading || busy" @click="load">刷新市场</button>
    </div>
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
          <span class="plugin-status">签名目录</span>
        </div>
        <h2>{{ entry.name }}</h2>
        <p class="plugin-id">{{ entry.id }} · v{{ entry.version }}</p>
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
        <UiButton :disabled="busy" @click="install(entry)">安装插件</UiButton>
      </article>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from "../components/UiButton.vue";
import { useRemoteResource } from "../composables/useRemoteResource";
import { confirmAction } from "../utils/feedback";
import {
  fetchPluginMarket,
  installMarketPlugin,
  surfaceLabel,
  type Market,
  type MarketEntry,
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
  surface = ref(""),
  busy = ref(false),
  actionError = ref(""),
  message = ref("");
const filtered = computed(() =>
  market.value.entries.filter(
    (e) =>
      `${e.name} ${e.description}`
        .toLowerCase()
        .includes(query.value.toLowerCase()) &&
      (!surface.value || e.surfaces.includes(surface.value as any)),
  ),
);
async function install(entry: MarketEntry) {
  if (
    busy.value ||
    !(await confirmAction({
      title: `安装 ${entry.name}`,
      message: `安装发布者 ${entry.publisher} 提供的 v${entry.version}。插件安装后保持停用。`,
      confirmText: "验证并安装",
    }))
  )
    return;
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await installMarketPlugin(entry);
    message.value = `${entry.name} 已安装，请到插件管理中配置和启用。`;
  } catch (e: any) {
    actionError.value =
      e?.response?.data?.message ||
      "安装失败，请检查市场连接、签名和插件版本。";
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
