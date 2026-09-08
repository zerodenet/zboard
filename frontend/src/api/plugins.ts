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
export interface Plugin {
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
    description?: string;
    surfaces: Surface[];
    capabilities: string[];
    components: { server?: unknown };
    contributions: { pages: PluginPage[] };
  };
  compatibility: { compatible: boolean; tested: boolean; reason: string };
  versions: PluginVersion[];
}
export interface MarketEntry {
  id: string;
  name: string;
  description: string;
  version: string;
  publisher: string;
  sha256: string;
  surfaces: Surface[];
}
export interface Market {
  configured: boolean;
  entries: MarketEntry[];
  expires_at: string;
}
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
export const importPlugin = (file: File) =>
  api
    .post("/admin/plugins", file, {
      headers: { "Content-Type": "application/octet-stream" },
      timeout: 60_000,
    })
    .then(data<Plugin>);
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
