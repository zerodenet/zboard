<template>
  <section class="plugin-frame" :class="{ 'plugin-frame--slot': !!slot }">
    <div v-if="error" class="plugin-state" role="alert">
      <div><strong>暂时无法显示此内容</strong><p>{{ error }}</p></div>
      <UiButton variant="secondary" size="sm" @click="load">重新加载</UiButton>
    </div>
    <div v-else-if="!session" class="plugin-state" role="status">正在加载…</div>
    <div v-if="connectionWarning" class="plugin-state plugin-state--connection" role="status">
      <p>连接暂时中断，正在尝试恢复。</p>
      <UiButton variant="secondary" size="sm" @click="verify">重试连接</UiButton>
    </div>
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
    <div v-if="passwordConfirmation" class="plugin-confirm-backdrop" role="presentation" @click.self="!confirming && cancelPasswordConfirmation()">
      <section class="plugin-confirm" role="dialog" aria-modal="true" aria-labelledby="plugin-confirm-title">
        <h2 id="plugin-confirm-title">确认账户密码</h2>
        <p>验证后 5 分钟内无需重复输入。密码不会提供给插件。</p>
        <label for="plugin-confirm-password">当前密码</label>
        <input id="plugin-confirm-password" v-model="confirmationPassword" type="password" autocomplete="current-password" maxlength="72" autofocus @keyup.enter="confirmPassword">
        <p v-if="confirmationError" role="alert">{{ confirmationError }}</p><div><UiButton variant="secondary" type="button" :disabled="confirming" @click="cancelPasswordConfirmation">取消</UiButton><UiButton type="button" :disabled="!confirmationPassword || confirming" @click="confirmPassword">{{ confirming ? "验证中…" : "确认" }}</UiButton></div>
      </section>
    </div>
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { accountConfirmation, clearAccountConfirmation, confirmAccountPassword } from "../api/accountSecurity";
import UiButton from "../components/UiButton.vue";
import { useAppStore } from "../stores/app";
import {
  createPluginSession,
  createPluginSlotSession,
  pluginBridge,
  revokePluginSession,
  type PluginSession,
  type Surface,
  type PluginSlotContribution,
} from "../api/plugins";
const props = defineProps<{
  pluginId: string;
  pageId?: string;
  slot?: PluginSlotContribution;
  targetUserId?: number;
  surface: Surface;
  configuration?: boolean;
  title?: string;
}>();
const app = useAppStore();
const frame = ref<HTMLIFrameElement | null>(null),
  session = ref<PluginSession | null>(null),
  error = ref(""),
  connectionWarning = ref(false),
  height = ref(props.slot ? 160 : 620);
let controller = new AbortController(),
  generation = 0,
  loaded = false,
  verifying = false,
  timer: ReturnType<typeof setInterval> | undefined;
const pending = new Set<string>();
const passwordConfirmation = ref(false), confirmationPassword = ref(""), confirmationError = ref(""), confirming = ref(false);
let passwordResolver: ((value: string) => void) | undefined,
  passwordRejecter: ((reason?: unknown) => void) | undefined;
function requestPasswordConfirmation() {
  if (passwordConfirmation.value) return Promise.reject(new Error("confirmation already active"));
  const proof = accountConfirmation(); if (proof) return Promise.resolve(proof);
  confirmationError.value = "";
  passwordConfirmation.value = true;
  confirmationPassword.value = "";
  return new Promise<string>((resolve, reject) => { passwordResolver = resolve; passwordRejecter = reject; });
}
function finishPasswordConfirmation() {
  passwordConfirmation.value = false;
  confirmationPassword.value = "";
  passwordResolver = undefined;
  passwordRejecter = undefined;
}
async function confirmPassword() {
  if (!confirmationPassword.value || !passwordResolver || confirming.value) return;
  const resolve = passwordResolver;
  confirming.value = true; confirmationError.value = "";
  try {
    const proof = await confirmAccountPassword(confirmationPassword.value);
    if (passwordResolver !== resolve) return;
    finishPasswordConfirmation(); resolve(proof);
  } catch (cause: any) {
    confirmationError.value = cause?.response?.data?.message || "密码验证失败，请重试。";
    confirmationPassword.value = "";
  } finally { confirming.value = false; }
}
function cancelPasswordConfirmation() {
  const reject = passwordRejecter;
  finishPasswordConfirmation();
  reject?.(new Error("confirmation cancelled"));
}
function clear() {
  if (passwordConfirmation.value) cancelPasswordConfirmation();
  if (session.value) void revokePluginSession(session.value).catch(() => {});
  generation++;
  controller.abort();
  controller = new AbortController();
  session.value = null;
  connectionWarning.value = false;
  pending.clear();
  loaded = false;
}
async function load() {
  clear();
  error.value = "";
  const current = generation;
  try {
    const value = props.slot
      ? await createPluginSlotSession(props.pluginId, props.slot, props.targetUserId || 0, controller.signal)
      : await createPluginSession(props.pluginId, props.pageId || "", props.surface, !!props.configuration, controller.signal);
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
      height.value = Math.max(props.slot ? 0 : 320, Math.min(1000, Math.ceil(message.height)));
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
    !["context.load", "config.load", "config.save", "config.test", "storage.get", "storage.put", "storage.delete", "identity.providers.list", "identity.bindings.list", "identity.login.start", "identity.bind.start", "identity.binding.unlink"].includes(
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
    const confirmedPassword = props.surface === "account" && (message.type === "identity.bind.start" || message.type === "identity.binding.unlink")
      ? await requestPasswordConfirmation()
      : "";
    const identityPayload = message.type === "identity.login.start"
      ? { provider_id: message.provider_id }
      : message.type === "identity.bind.start"
        ? { provider_id: message.provider_id,  }
        : message.type === "identity.binding.unlink"
          ? { identity_id: message.identity_id,  }
          : {};
    const payload = message.type.startsWith("identity.")
      ? identityPayload
      : message.type.startsWith("storage.") ? { key: message.key, revision: message.revision, value: message.value } :
      message.type === "config.save"
        ? { revision: message.revision, config: message.config }
        : {};
    if (JSON.stringify(payload).length > 65_536) throw new Error("too large");
    const result = confirmedPassword
      ? await pluginBridge(s, message.type, payload, controller.signal, confirmedPassword)
      : await pluginBridge(s, message.type, payload, controller.signal);
    if (message.type === "identity.login.start" || message.type === "identity.bind.start") {
      const raw = typeof result.authorization_url === "string" ? result.authorization_url : "";
      const target = new URL(raw);
      const localHTTP = target.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname);
      if (target.username || target.password || (target.protocol !== "https:" && !localHTTP)) throw new Error("unsafe navigation");
      window.location.assign(target.href);
    }
    respond({ ok: true, result });
  } catch (cause: any) {
    if (cause?.response?.status === 401) clearAccountConfirmation();
    respond({ ok: false, error: cause?.response?.data?.message || "操作已取消或未完成，请重试。" });
  } finally {
    if (current === generation) pending.delete(message.request_id);
  }
}
async function verify() {
  const s = session.value;
  if (!s || document.hidden || verifying) return;
  verifying = true;
  try {
    await pluginBridge(s, "context.load", {}, controller.signal);
    if (session.value?.token === s.token) connectionWarning.value = false;
  } catch (cause: any) {
    if (session.value?.token !== s.token) return;
    if ([401, 403].includes(cause?.response?.status)) {
      clear();
      error.value = "此内容已过期或访问权限已变化，请重新加载。";
    } else {
      // A failed connectivity check must not discard an in-progress form.
      connectionWarning.value = true;
    }
  } finally {
    verifying = false;
  }
}
watch(
  () => [props.pluginId, props.pageId, props.slot?.id, props.slot?.slot, props.surface, props.configuration, props.targetUserId],
  load,
);
watch(
  () => app.token,
  () => {
    clearAccountConfirmation(); clear();
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
  display: block;
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface);
}
.plugin-frame--slot iframe { border: 0; border-radius: 0; background: transparent; }
.plugin-state { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 16px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); color: var(--muted); }
.plugin-state strong { color: var(--text); font-size: 14px; }
.plugin-state p { margin: 4px 0 0; line-height: 1.6; }
.plugin-state button { flex-shrink: 0; }
.plugin-state--connection { margin-bottom: 12px; }
@media (max-width: 520px) { .plugin-state { align-items: flex-start; flex-direction: column; } }
.plugin-confirm-backdrop { position: fixed; z-index: 10000; inset: 0; display: grid; place-items: center; padding: 20px; background: var(--navigation-scrim); }
.plugin-confirm { width: min(420px, 100%); padding: 22px; border: 1px solid var(--line); border-radius: 14px; background: var(--surface); box-shadow: 0 20px 60px var(--sidebar-shadow); }
.plugin-confirm h2 { margin: 0 0 6px; }
.plugin-confirm p { padding: 0; margin: 0 0 16px; }
.plugin-confirm label { display: block; margin-bottom: 6px; font-weight: 650; }
.plugin-confirm input { width: 100%; padding: 10px 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); color: inherit; }
.plugin-confirm div { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
</style>
