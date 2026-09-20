import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PluginFrame from "./PluginFrame.vue";
const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  createSlot: vi.fn(),
  bridge: vi.fn(),
  revoke: vi.fn(),
}));
const security = vi.hoisted(() => ({ proof: '', confirm: vi.fn() }));
vi.mock('../api/accountSecurity', () => ({ accountConfirmation: () => security.proof, clearAccountConfirmation: () => { security.proof = '' }, confirmAccountPassword: security.confirm }));
vi.mock("../stores/app", () => ({ useAppStore: () => ({ token: "" }) }));
vi.mock("../api/plugins", () => ({
  createPluginSession: mocks.create,
  createPluginSlotSession: mocks.createSlot,
  pluginBridge: mocks.bridge,
  revokePluginSession: mocks.revoke,
}));
const session = {
  token: "asset-token",
  bridge_token: "secret",
  url: "/api/v1/plugin-assets/asset-token/ui/index.html",
  purpose: "business",
  generation: 2,
};
const render = () =>
  mount(PluginFrame, {
    props: { pluginId: "example.welcome", pageId: "home", surface: "public" },
  });
function send(
  wrapper: ReturnType<typeof render>,
  type: string,
  changes: Record<string, unknown> = {},
  source?: Window,
) {
  const event = new MessageEvent("message", {
    data: {
      source: "zboard-plugin-ui",
      bridge_token: "secret",
      request_id: "request1",
      type,
      ...changes,
    },
  });
  Object.defineProperty(event, "source", {
    value: source || wrapper.get("iframe").element.contentWindow,
  });
  window.dispatchEvent(event);
}
describe("plugin iframe boundary", () => {
  beforeEach(() => {
    security.proof = '';
    security.confirm.mockImplementation(async () => { security.proof = 'host-proof'; return security.proof });
    mocks.create.mockResolvedValue({ ...session });
    mocks.createSlot.mockResolvedValue({ ...session, purpose: "slot", slot: "account.security.identities" });
    mocks.bridge.mockResolvedValue({ surface: "public" });
    mocks.revoke.mockResolvedValue({});
  });
  it("isolates the frame and rejects other windows, bad tokens and core commands", async () => {
    const wrapper = render();
    await flushPromises();
    expect(wrapper.get("iframe").attributes("sandbox")).toBe("allow-scripts");
    send(wrapper, "context.load", {}, window);
    send(wrapper, "context.load", { bridge_token: "wrong" });
    send(wrapper, "users.credentials.rotate");
    send(wrapper, "storage.get", { key: "private" });
    send(wrapper, "config.save", { config: {} });
    await flushPromises();
    expect(mocks.bridge).not.toHaveBeenCalled();
    send(wrapper, "context.load");
    await flushPromises();
    expect(mocks.bridge).toHaveBeenCalledTimes(1);
    wrapper.unmount();
    expect(mocks.revoke).toHaveBeenCalledWith(
      expect.objectContaining({ token: session.token }),
    );
  });
  it("revokes a session returned after its component was removed", async () => {
    let resolve!: (value: typeof session) => void;
    mocks.create.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const wrapper = render();
    wrapper.unmount();
    resolve(session);
    await flushPromises();
    expect(mocks.revoke).toHaveBeenCalledWith(session);
  });
  it("closes the frame after navigation and after host revocation", async () => {
    const wrapper = render();
    await flushPromises();
    await wrapper.get("iframe").trigger("load");
    await wrapper.get("iframe").trigger("load");
    await flushPromises();
    expect(wrapper.find("iframe").exists()).toBe(false);
    expect(mocks.revoke).toHaveBeenCalled();
    wrapper.unmount();
  });
  it("forwards only the administrator session storage key, revision and value", async () => {
    security.proof = '';
    security.confirm.mockImplementation(async () => { security.proof = 'host-proof'; return security.proof });
    mocks.create.mockResolvedValue({ ...session, purpose: 'business' });
    const wrapper = mount(PluginFrame, { props: { pluginId: 'example.storage', pageId: 'home', surface: 'admin' } });
    await flushPromises();
    send(wrapper, 'storage.put', { key: 'cursor', revision: 2, value: { offset: 10 }, plugin_id: 'other.plugin' });
    await flushPromises();
    expect(mocks.bridge).toHaveBeenCalledWith(expect.anything(), 'storage.put', { key: 'cursor', revision: 2, value: { offset: 10 } }, expect.any(AbortSignal));
    wrapper.unmount();
  });
  it("creates a target-scoped slot session and forwards only identity bridge fields", async () => {
    const slot = { id: "admin-user-identities", surface: "admin" as const, slot: "admin.user.identities", title: "OAuth", entrypoint: "ui/admin-user.html" };
    const wrapper = mount(PluginFrame, { props: { pluginId: "zboard.oauth", slot, surface: "admin", targetUserId: 9 } });
    await flushPromises();
    expect(mocks.createSlot).toHaveBeenCalledWith("zboard.oauth", slot, 9, expect.any(AbortSignal));
    send(wrapper, "identity.bindings.list", { target_user_id: 88, password: "ignored", plugin_id: "other.plugin" });
    await flushPromises();
    expect(mocks.bridge).toHaveBeenCalledWith(expect.anything(), "identity.bindings.list", {}, expect.any(AbortSignal));
    wrapper.unmount();
  });
  it("collects account confirmation in the host and never trusts a plugin-supplied password", async () => {
    const slot = { id: "account-identities", surface: "account" as const, slot: "account.security.identities", title: "OAuth", entrypoint: "ui/account.html" };
    const wrapper = mount(PluginFrame, { props: { pluginId: "zboard.oauth", slot, surface: "account" } });
    await flushPromises();
    send(wrapper, "identity.binding.unlink", { identity_id: "binding", password: "plugin-controlled" });
    await flushPromises();
    expect(mocks.bridge).not.toHaveBeenCalled();
    await wrapper.get("#plugin-confirm-password").setValue("host-confirmed");
    await wrapper.findAll(".plugin-confirm button")[1].trigger("click");
    await flushPromises();
    expect(mocks.bridge).toHaveBeenCalledWith(expect.anything(), "identity.binding.unlink", { identity_id: "binding" }, expect.any(AbortSignal), "host-proof");
    expect(security.confirm).toHaveBeenCalledWith("host-confirmed");
    send(wrapper, "identity.binding.unlink", { identity_id: "second" });
    await flushPromises();
    expect(wrapper.find('#plugin-confirm-password').exists()).toBe(false);
    expect(security.confirm).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("retains the frame on a network failure and clears the warning after recovery", async () => {
    const wrapper = render();
    await flushPromises();
    const frame = wrapper.get('iframe').element;
    mocks.bridge.mockRejectedValueOnce(new Error('Network Error'));
    window.dispatchEvent(new Event('focus'));
    await flushPromises();
    expect(wrapper.get('iframe').element).toBe(frame);
    expect(wrapper.text()).toContain('连接暂时中断');
    window.dispatchEvent(new Event('focus'));
    await flushPromises();
    expect(wrapper.text()).not.toContain('连接暂时中断');
    wrapper.unmount();
  });
  it("closes invalid sessions and offers a styled reload action", async () => {
    const wrapper = render();
    await flushPromises();
    mocks.bridge.mockRejectedValueOnce({ response: { status: 403 } });
    window.dispatchEvent(new Event('focus'));
    await flushPromises();
    expect(wrapper.find('iframe').exists()).toBe(false);
    expect(wrapper.get('[role="alert"]').text()).toContain('访问权限已变化');
    await wrapper.get('[role="alert"] button').trigger('click');
    await flushPromises();
    expect(wrapper.find('iframe').exists()).toBe(true);
    wrapper.unmount();
  });
  it("allows compact slots to shrink to their intrinsic content height", async () => {
    const slot = { id: 'login', surface: 'public' as const, slot: 'auth.login.methods', title: 'OAuth', entrypoint: 'ui/login.html' };
    const wrapper = mount(PluginFrame, { props: { pluginId: 'zboard.oauth', slot, surface: 'public' } });
    await flushPromises();
    send(wrapper, 'ui.resize', { height: 42 });
    await flushPromises();
    expect(wrapper.get('iframe').attributes('style')).toContain('42px');
    wrapper.unmount();
  });

});
