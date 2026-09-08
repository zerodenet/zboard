import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PluginFrame from "./PluginFrame.vue";
const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  bridge: vi.fn(),
  revoke: vi.fn(),
}));
vi.mock("../stores/app", () => ({ useAppStore: () => ({ token: "" }) }));
vi.mock("../api/plugins", () => ({
  createPluginSession: mocks.create,
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
    mocks.create.mockResolvedValue({ ...session });
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
});
