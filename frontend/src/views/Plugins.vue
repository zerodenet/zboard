<template>
  <div class="plugins-page">
    <header class="plugins-heading">
      <div>
        <p class="eyebrow">扩展中心</p>
        <h1>插件管理</h1>
        <p>
          管理前台与后台扩展。插件停用后，已提交的核心业务仍由系统继续处理。
        </p>
      </div>
      <div class="plugins-actions">
        <RouterLink class="button button-secondary" to="/admin/plugin-market"
          >浏览插件市场</RouterLink
        ><UiFileUpload choose-label="离线导入" accept=".zbplugin" :max-file-size="32 * 1024 * 1024" :disabled="busy" @select="importFiles" />
      </div>
    </header>
    <TransientFeedback :success="message" />
    <p v-if="error || actionError" role="alert" class="plugins-error">
      {{ actionError || error }}
    </p>
    <div class="plugins-toolbar">
      <UiInput
        v-model="query"
        placeholder="搜索插件名称或 ID"
        aria-label="搜索插件"
      /><UiSelect v-model="surface" aria-label="使用范围" :options="surfaceOptions" /><button type="button" :disabled="loading || busy" @click="load">
        刷新</button
      ><span>{{ items.length }} 个插件</span>
    </div>
    <p v-if="loading && !items.length">正在读取插件…</p>
    <section v-else-if="!filtered.length" class="plugins-empty">
      <h2>{{ items.length ? "没有匹配的插件" : "还没有安装插件" }}</h2>
      <p>从插件市场选择扩展，或导入发布者提供的签名 .zbplugin 文件。</p>
      <p>离线导入不需要连接市场，安装后默认停用。</p>
    </section>
    <div class="plugin-grid">
      <article v-for="plugin in filtered" :key="plugin.id" class="plugin-card">
        <div class="plugin-card-top">
          <div class="plugin-monogram">{{ plugin.name.slice(0, 1) }}</div>
          <span class="plugin-status" :class="plugin.state">{{
            pluginStateLabel(plugin.state)
          }}</span>
        </div>
        <h2>{{ plugin.name }}</h2>
        <p class="plugin-id">{{ plugin.id }} · v{{ plugin.version }}</p>
        <p>{{ plugin.manifest.description || "此插件未提供描述。" }}</p>
        <div class="plugin-tags">
          <span v-for="s in plugin.manifest.surfaces" :key="s">{{
            surfaceLabel(s)
          }}</span
          ><span>{{
            plugin.manifest.components.server ? "包含服务端组件" : "页面扩展"
          }}</span>
        </div>
        <dl>
          <div>
            <dt>签名发布者</dt>
            <dd>{{ plugin.publisher }}</dd>
          </div>
          <div>
            <dt>版本检查</dt>
            <dd>
              {{
                !plugin.compatibility.compatible
                  ? "当前宿主不兼容"
                  : plugin.compatibility.tested
                    ? "发布者已测试当前版本"
                    : "兼容范围内，尚未声明测试"
              }}
            </dd>
          </div>
        </dl>
        <p v-if="plugin.last_error" class="plugins-error">
          {{ plugin.last_error }}
        </p>
        <div class="plugin-card-actions">
          <UiButton
            v-if="plugin.enabled"
            variant="secondary"
            :disabled="busy"
            @click="act(plugin, 'disable')"
            >停用</UiButton
          ><UiButton
            v-else-if="plugin.state !== 'uninstalled'"
            :disabled="busy || !plugin.compatibility.compatible"
            @click="act(plugin, 'enable')"
            >启用</UiButton
          ><UiButton variant="ghost" :disabled="busy" @click="inspect(plugin)"
            >配置与记录</UiButton
          ><UiButton
            v-if="!plugin.enabled && plugin.state !== 'uninstalled'"
            variant="ghost"
            :disabled="busy"
            @click="act(plugin, 'uninstall')"
            >卸载</UiButton
          >
        </div>
      </article>
    </div>
    <section v-if="selected" class="plugin-detail">
      <header>
        <div>
          <p class="eyebrow">配置与运行记录</p>
          <h2>{{ selected.name }}</h2>
        </div>
        <button aria-label="关闭插件详情" @click="selected = null">关闭</button>
      </header>
      <p v-if="hasConfig">
        配置加密保存，已存值不会回显。提交下方 JSON 会完整替换旧配置。
      </p>
      <p v-if="!hasConfig">此插件未声明配置能力。</p>
      <div v-if="hasConfig && selected.state !== 'uninstalled'">
        <label for="plugin-config">插件配置 JSON</label
        ><UiTextarea
          id="plugin-config"
          v-model="configText"
          rows="7"
          spellcheck="false"
        />
        <div class="plugins-actions">
          <UiButton
            :disabled="busy || !selected.compatibility.compatible"
            @click="save"
            >保存配置</UiButton
          ><UiButton
            v-if="selected.manifest.components.server"
            variant="secondary"
            :disabled="busy || !selected.compatibility.compatible"
            @click="test"
            >测试已保存配置</UiButton
          >
        </div>
        <PluginFrame
          v-if="configPage && selected.compatibility.compatible"
          :plugin-id="selected.id"
          :page-id="configPage.id"
          surface="admin"
          configuration
          title="插件配置页面"
        />
      </div>
      <template
        v-if="
          !selected.enabled &&
          selected.state !== 'uninstalled' &&
          selected.versions.length > 1
        "
        ><h3>历史版本</h3>
        <p>切换后保持停用；旧版本必须能够验证当前配置。</p>
        <div
          v-for="version in selected.versions.filter(
            (v) => v.id !== selected?.version_id,
          )"
          :key="version.id"
          class="plugin-history"
        >
          <span>v{{ version.version }}</span
          ><button
            :disabled="busy"
            @click="act(selected, 'rollback', version.id)"
          >
            恢复此版本
          </button>
        </div></template
      >
      <UiButton
        v-if="selected.state === 'uninstalled'"
        variant="danger"
        :disabled="busy"
        @click="act(selected, 'purge')"
        >删除保留的配置</UiButton
      >
      <h3>最近操作</h3>
      <p v-if="!operations.length">暂无操作记录</p>
      <div v-for="op in operations" :key="op.id" class="plugin-history">
        <span
          >{{ op.action }} · {{ op.state
          }}<small>{{ op.actor }} · {{ op.created_at }}</small></span
        ><span>{{ op.message }}</span>
      </div>
    </section>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import UiFileUpload from '../components/UiFileUpload.vue'
import UiTextarea from '../components/UiTextarea.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from "../components/UiButton.vue";
import PluginFrame from "../plugins/PluginFrame.vue";
import { useRemoteResource } from "../composables/useRemoteResource";
import { confirmAction } from "../utils/feedback";
import {
  fetchPlugins,
  importPlugin,
  pluginAction,
  fetchPluginConfig,
  savePluginConfig,
  testPluginConfig,
  fetchPluginOperations,
  surfaceLabel,
  pluginStateLabel,
  type Plugin,
} from "../api/plugins";
import "../styles/plugins.css";
const {
  data: items,
  loading,
  error,
  load,
} = useRemoteResource<Plugin[]>({
  initial: () => [],
  fetch: ({ signal }) => fetchPlugins(signal),
  errorMessage: "无法读取插件列表，请重试。",
});
const surfaceOptions = [{ label: '所有范围', value: '' }, { label: '公开前台', value: 'public' }, { label: '用户前台', value: 'account' }, { label: '管理后台', value: 'admin' }]
const query = ref(""),
  surface = ref(""),
  busy = ref(false),
  actionError = ref(""),
  message = ref(""),
  selected = ref<Plugin | null>(null),
  configText = ref("{}"),
  configRevision = ref(0);
const operations = ref<Awaited<ReturnType<typeof fetchPluginOperations>>>([]);
const filtered = computed(() =>
  items.value.filter(
    (p) =>
      `${p.name} ${p.id}`.toLowerCase().includes(query.value.toLowerCase()) &&
      (!surface.value || p.manifest.surfaces.includes(surface.value as any)),
  ),
);
const hasConfig = computed(() =>
  selected.value?.manifest.capabilities.includes("zboard.config.v1"),
);
const configPage = computed(() =>
  selected.value?.manifest.contributions.pages.find(
    (p) => p.surface === "admin" && p.purpose === "configuration",
  ),
);
async function run(fn: () => Promise<unknown>, success: string) {
  if (busy.value) return;
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await fn();
    message.value = success;
    await load();
    if (selected.value)
      selected.value =
        items.value.find((p) => p.id === selected.value?.id) || null;
  } catch (e: any) {
    actionError.value =
      e?.response?.data?.message ||
      e?.message ||
      "插件操作失败，请检查签名、兼容版本和运行状态。";
  } finally {
    busy.value = false;
  }
}
async function importFiles(files: File[]) {
  const file = files[0];
  if (!file) return;
  if (file.size > 32 * 1024 * 1024) {
    actionError.value = "插件包不能超过 32 MiB。";
    return;
  }
  await run(
    () => importPlugin(file),
    "插件已导入并保持停用，请检查配置后启用。",
  );
}
async function act(plugin: Plugin, action: string, versionID = "") {
  const untested = action === "enable" && !plugin.compatibility.tested;
  const labels: Record<string, string> = {
    enable: "启用",
    disable: "停用",
    uninstall: "卸载",
    rollback: "恢复版本",
    purge: "删除配置",
  };
  if (
    !(await confirmAction({
      title: `${labels[action]} ${plugin.name}`,
      message:
        action === "purge"
          ? "永久删除插件保留的配置。核心用户、订单和审计记录不受影响。"
          : action === "uninstall"
            ? "删除插件程序和页面，保留配置与操作记录。"
            : untested
              ? "发布者尚未声明测试当前宿主版本。确认信任该插件后启用。"
              : `${labels[action]}此插件？核心业务仍由 ZBoard 管理。`,
      tone: action === "purge" ? "danger" : "primary",
      confirmText: labels[action],
    }))
  )
    return;
  await run(
    () => pluginAction(plugin, action, untested, versionID),
    `插件操作已完成：${labels[action]}。`,
  );
  if (selected.value)
    await run(async () => {
      operations.value = await fetchPluginOperations(selected.value!.id);
    }, message.value);
}
async function inspect(plugin: Plugin) {
  if (busy.value) return;
  selected.value = plugin;
  configText.value = "{}";
  await run(async () => {
    const [config, ops] = await Promise.all([
      fetchPluginConfig(plugin.id),
      fetchPluginOperations(plugin.id),
    ]);
    configRevision.value = config.revision;
    operations.value = ops;
  }, "");
}
async function save() {
  const p = selected.value;
  if (!p) return;
  let config: unknown;
  try {
    config = JSON.parse(configText.value);
    if (!config || Array.isArray(config) || typeof config !== "object")
      throw new Error();
  } catch {
    actionError.value = "请输入有效的 JSON 对象。";
    return;
  }
  await run(async () => {
    const view = await savePluginConfig(p.id, configRevision.value, config);
    configRevision.value = view.revision;
    configText.value = "{}";
  }, "配置已加密保存。");
}
async function test() {
  if (selected.value)
    await run(() => testPluginConfig(selected.value!.id), "插件配置测试通过。");
}
onMounted(load);
</script>
