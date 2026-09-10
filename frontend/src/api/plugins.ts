import { authenticatedAPI as api } from "./client";
export type Surface = "public" | "account" | "admin";
export interface PluginPage {
  id: string;
  surface: Surface;
  title: string;
  purpose?: string;
}
export interface PluginVersion {
  id: string;
  version: string;
  digest: string;
  created_at: string;
}
export interface PluginMigration { id: string; epoch: number; version: number; checksum: string; digest: string; actor: string; created_at: string }
export interface Plugin {
  admission: { accepted: boolean; capabilities: string[] };
  data: { version: number; target_version: number; epoch: number; revision: number; stored: boolean; compatible: boolean; migration_required: boolean };
  id: string;
  name: string;
  version: string;
  version_id: string;
  digest: string;
  publisher: string;
  state: string;
  enabled: boolean;
  generation: number;
  config_revision: number;
  last_error: string;
  manifest: {
    data?: { version: number; min_compatible_version: number; migrations: Array<{ version: number; changes: Array<{ target: string; operation: string; key: string; to?: string }> }> };
    description?: string;
    surfaces: Surface[];
    capabilities: string[];
    components: { server?: unknown };
    contributions: { pages: PluginPage[] };
  };
  compatibility: { compatible: boolean; tested: boolean; reason: string; warning?: string };
  versions: PluginVersion[];
}
export interface MarketEntry {
  discovery_only?: boolean;
  repository?: string;
  id: string;
  name: string;
  description: string;
  version: string;
  publisher: string;
  sha256: string;
  surfaces: Surface[];
}
export interface Market {
  kind?: "registry" | "signed";
  source_url?: string;
  configured: boolean;
  entries: MarketEntry[];
  expires_at: string;
}
export interface MarketArtifact { platform: string; url: string; sha256: string; size: number }
export interface MarketDetail {
  entry: MarketEntry; platform: string; notice?: string;
  release?: { version: string; artifacts: MarketArtifact[] };
  installed?: { version: string; digest: string; state: string };
}
export const fetchMarketDetail = (id: string, signal?: AbortSignal) => api.get(`/admin/plugin-market/${encodeURIComponent(id)}`, { signal, timeout: 60_000 }).then(data<MarketDetail>);
export const previewMarketPlugin = (id: string, signal?: AbortSignal) => api.post(`/admin/plugin-market/${encodeURIComponent(id)}/inspect`, undefined, { signal, timeout: 60_000 }).then(data<ImportPreview>);
export const confirmMarketPlugin = (id: string, preview: ImportPreview, trust: boolean) => api.post(`/admin/plugin-market/${encodeURIComponent(id)}/install`, { digest: preview.digest, fingerprint: trust ? preview.fingerprint : '' }, { timeout: 60_000 }).then(data<Plugin>);
export interface PluginSession {
  token: string;
  bridge_token: string;
  plugin_id: string;
  page_id: string;
  surface: Surface;
  purpose: string;
  url: string;
  generation: number;
  expires_at: string;
}
export interface CatalogPage {
  plugin_id: string;
  page: PluginPage;
  generation: number;
}
export interface ConfigView {
  revision: number;
  configured: boolean;
}
const data = <T>(r: { data: { data: T } }) => r.data.data;
export const fetchPlugins = (signal?: AbortSignal) =>
  api.get("/admin/plugins", { signal }).then(data<Plugin[]>);
export interface ImportPreview {
  manifest: Plugin['manifest'] & { id: string; name: string; version: string };
  publisher: string; public_key: string; fingerprint: string; digest: string; trusted: boolean;
  compatibility: Plugin['compatibility'];
}
export const previewPlugin = (file: File, publicKey = '') => api.post('/admin/plugins', file, {
  params: { inspect: true }, headers: { 'Content-Type': 'application/octet-stream', 'X-Plugin-Public-Key': publicKey.trim() }, timeout: 60_000,
}).then(data<ImportPreview>);
export const importPlugin = (file: File, preview?: ImportPreview, trust = false) => api.post('/admin/plugins', file, {
  headers: { 'Content-Type': 'application/octet-stream', 'X-Plugin-Public-Key': preview?.public_key || '',
    'X-Plugin-Digest': preview?.digest || '', 'X-Plugin-Trust-Fingerprint': trust ? preview?.fingerprint || '' : '' },
  timeout: 60_000,
}).then(data<Plugin>);
export const pluginAction = (
  p: Plugin,
  action: string,
  acceptUntested = false,
  versionID = "",
) =>
  api
    .post(
      `/admin/plugins/${p.id}/actions/${action}`,
      {
        generation: p.generation,
        accept_untested: acceptUntested,
        version_id: versionID,
      },
      { timeout: 60_000 },
    )
    .then(data<Plugin>);
export const fetchPluginConfig = (id: string, signal?: AbortSignal) =>
  api.get(`/admin/plugins/${id}/config`, { signal }).then(data<ConfigView>);
export const savePluginConfig = (
  id: string,
  revision: number,
  config: unknown,
) =>
  api
    .put(`/admin/plugins/${id}/config`, { revision, config })
    .then(data<ConfigView>);
export const testPluginConfig = (id: string) =>
  api.post(`/admin/plugins/${id}/test`);
export const fetchPluginOperations = (id: string, signal?: AbortSignal) =>
  api
    .get(`/admin/plugins/${id}/operations`, { signal })
    .then(
      data<
        Array<{
          id: string;
          action: string;
          state: string;
          actor: string;
          message: string;
          created_at: string;
        }>
      >,
    );
export const fetchPluginMarket = (signal?: AbortSignal) =>
  api.get("/admin/plugin-market", { signal }).then(data<Market>);
export const installMarketPlugin = (entry: MarketEntry) =>
  api
    .post(
      "/admin/plugin-market",
      { id: entry.id, digest: entry.sha256 },
      { timeout: 60_000 },
    )
    .then(data<Plugin>);
export const fetchPluginPages = (surface: Surface, signal?: AbortSignal) =>
  api
    .get("/plugin-ui/catalog", { params: { surface }, signal })
    .then(data<CatalogPage[]>);
export const createPluginSession = (
  id: string,
  page: string,
  surface: Surface,
  configuration = false,
  signal?: AbortSignal,
) =>
  api
    .post(
      `/plugin-ui/${id}/session`,
      { page, surface, configuration },
      { signal },
    )
    .then(data<PluginSession>);
export const pluginBridge = (
  session: PluginSession,
  type: string,
  payload: Record<string, unknown> = {},
  signal?: AbortSignal,
) =>
  api
    .post(
      "/plugin-ui/bridge",
      { ...payload, type },
      { headers: { "X-Plugin-Session": session.token }, signal },
    )
    .then(data<Record<string, unknown>>);
export const pluginPagePath = (surface: Surface, id: string, page: string) =>
  `${surface === "public" ? "" : `/${surface}`}/extensions/${encodeURIComponent(id)}/${encodeURIComponent(page)}`;
export const surfaceLabel = (s: Surface) =>
  ({ public: "公开前台", account: "用户前台", admin: "管理后台" })[s];
export const pluginStateLabel = (s: string) =>
  ({
    active: "已启用",
    disabled: "已停用",
    failed: "运行异常",
    incompatible: "不兼容",
    uninstalled: "已卸载",
  })[s] || s;

export const revokePluginSession = (session: PluginSession) =>
  api.delete("/plugin-ui/session", {
    headers: { "X-Plugin-Session": session.token },
  });

export const fetchPluginMigrations = (id: string, signal?: AbortSignal) => api.get(`/admin/plugins/${id}/migrations`, { signal }).then(data<PluginMigration[]>);
export const capabilityLabel = (c: string) => ({ 'zboard.ui.page.v1': '展示插件页面', 'zboard.config.v1': '管理自身配置', 'zboard.identity.provider.v1': '验证第三方身份', 'zboard.storage.v1': '读写自身私有数据' })[c] || c;
export const pluginBusinessLabel = (p: Plugin) => p.manifest.capabilities.includes('zboard.identity.provider.v1') ? '第三方登录与注册' : p.manifest.components.server ? '服务扩展' : '页面扩展';
