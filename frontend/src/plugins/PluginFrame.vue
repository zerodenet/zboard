<template>
  <section class="plugin-frame">
    <p v-if="error" role="alert">
      {{ error }} <button type="button" @click="load">重新加载</button>
    </p>
    <p v-else-if="!session" role="status">正在加载扩展…</p>
    <iframe
      v-if="session"
      ref="frame"
      :key="session.token"
      :src="session.url"
      :title="title || '插件页面'"
      sandbox="allow-scripts"
      referrerpolicy="no-referrer"
      :style="{ height: `${height}px` }"
      @load="onLoad"
    />
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useAppStore } from "../stores/app";
import {
  createPluginSession,
  pluginBridge,
  revokePluginSession,
  type PluginSession,
  type Surface,
} from "../api/plugins";
const props = defineProps<{
  pluginId: string;
  pageId: string;
  surface: Surface;
  configuration?: boolean;
  title?: string;
}>();
const app = useAppStore();
const frame = ref<HTMLIFrameElement | null>(null),
  session = ref<PluginSession | null>(null),
  error = ref(""),
  height = ref(620);
let controller = new AbortController(),
  generation = 0,
  loaded = false,
  timer: ReturnType<typeof setInterval> | undefined;
const pending = new Set<string>();
function clear() {
  if (session.value) void revokePluginSession(session.value).catch(() => {});
  generation++;
  controller.abort();
  controller = new AbortController();
  session.value = null;
  pending.clear();
  loaded = false;
}
async function load() {
  clear();
  error.value = "";
  const current = generation;
  try {
    const value = await createPluginSession(
      props.pluginId,
      props.pageId,
      props.surface,
      !!props.configuration,
      controller.signal,
    );
    if (current === generation) session.value = value;
    else void revokePluginSession(value).catch(() => {});
  } catch {
    if (current === generation)
      error.value = "扩展不可用、已停用或当前账户无权访问。";
  }
}
function onLoad() {
  if (loaded) {
    clear();
    error.value = "扩展页面已导航，会话已关闭。";
  } else loaded = true;
}
async function receive(event: MessageEvent) {
  const s = session.value,
    message = event.data;
  if (
    !s ||
    event.source !== frame.value?.contentWindow ||
    !message ||
    message.source !== "zboard-plugin-ui" ||
    message.bridge_token !== s.bridge_token
  )
    return;
  if (typeof message.type !== "string") return;
  if (message.type === "ui.resize") {
    if (Number.isFinite(message.height))
      height.value = Math.max(320, Math.min(1000, message.height));
    return;
  }
  if (message.type === "plugin.ready") return;
  if (
    typeof message.request_id !== "string" ||
    !/^[a-zA-Z0-9_-]{1,80}$/.test(message.request_id) ||
    pending.size >= 8 ||
    pending.has(message.request_id)
  )
    return;
  if (
    !["context.load", "config.load", "config.save", "config.test", "storage.get", "storage.put", "storage.delete"].includes(
      message.type,
    )
  )
    return;
  const current = generation;
  pending.add(message.request_id);
  const respond = (value: Record<string, unknown>) => {
    if (current === generation && session.value?.token === s.token)
      frame.value?.contentWindow?.postMessage(
        {
          source: "zboard-plugin-host",
          bridge_token: s.bridge_token,
          request_id: message.request_id,
          ...value,
        },
        "*",
      );
  };
  try {
    if (message.type.startsWith("config.") && s.purpose !== "configuration")
      throw new Error("denied");
    if (message.type.startsWith("storage.") && props.surface !== "admin") throw new Error("denied");
    const payload = message.type.startsWith("storage.") ? { key: message.key, revision: message.revision, value: message.value } :
      message.type === "config.save"
        ? { revision: message.revision, config: message.config }
        : {};
    if (JSON.stringify(payload).length > 65_536) throw new Error("too large");
    const result = await pluginBridge(
      s,
      message.type,
      payload,
      controller.signal,
    );
    respond({ ok: true, result });
  } catch {
    respond({ ok: false, error: "扩展请求被拒绝或执行失败。" });
  } finally {
    if (current === generation) pending.delete(message.request_id);
  }
}
async function verify() {
  const s = session.value;
  if (!s || document.hidden) return;
  try {
    await pluginBridge(s, "context.load", {}, controller.signal);
  } catch {
    if (session.value?.token === s.token) {
      clear();
      error.value = "扩展会话已结束，请重新加载。";
    }
  }
}
watch(
  () => [props.pluginId, props.pageId, props.surface, props.configuration],
  load,
);
watch(
  () => app.token,
  () => {
    clear();
    error.value = "登录状态已变化，请重新加载扩展。";
  },
);
onMounted(() => {
  window.addEventListener("message", receive);
  window.addEventListener("focus", verify);
  timer = setInterval(verify, 15_000);
  void load();
});
onBeforeUnmount(() => {
  clear();
  if (timer) clearInterval(timer);
  window.removeEventListener("message", receive);
  window.removeEventListener("focus", verify);
});
</script>
<style scoped>
.plugin-frame {
  min-width: 0;
  width: 100%;
}
.plugin-frame iframe {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface);
}
.plugin-frame p {
  padding: 24px;
  color: var(--muted);
}
.plugin-frame button {
  margin-left: 12px;
}
</style>
