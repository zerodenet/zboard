# ZBoard

[English](README.md) | [简体中文](README.zh-CN.md)

**A self-hosted panel for essential Zero proxy service management.**

ZBoard brings node deployment, protocol services, users, subscriptions, basic orders, and traffic usage into one web console. It is designed for personal deployments and small services that need to manage access across multiple nodes.

Administrators configure services and grant access; users get a separate account area to manage their subscriptions, copy client configurations, check usage, and contact support. [Zero Core](https://github.com/zerodenet/core) runs on the nodes and handles proxy traffic.

[Get started](https://docs.zerodenet.org/projects/zboard/guides/installation) · [Downloads](https://github.com/zerodenet/zboard/releases) · [Documentation](https://docs.zerodenet.org/projects/zboard/guides/) · [Report an issue](https://github.com/zerodenet/zboard/issues)

## What you can do

| Area | Capabilities |
| --- | --- |
| Nodes | Manage server assets and SSH access, install Zero, publish configurations, and inspect deployment results. |
| Protocol services | Configure VLESS, VMess, Shadowsocks, Trojan, Hysteria2, and Mieru; organize access through node groups. Protocol availability depends on the installed Zero build. |
| Network fronting | Expose a service through a forwarding node, with an optional shared proxy pool for that node's forwarding entries. |
| Subscriptions | Deliver Zero, Clash/Mihomo, and sing-box configurations; customize templates, routing rules, policy groups, and node filters. |
| Users and orders | Manage plans, billing options, orders, renewals, subscription periods, and traffic allowances. Administrators can confirm orders to activate access. |
| Usage and support | Track subscription traffic, review operational tasks and audit records, publish announcements, and handle support tickets. |

ZBoard focuses on essential panel functions. Online payments and other capabilities beyond this foundation are implemented through optional plugins.

## Get started

The published Docker image includes the backend and web console. A Linux amd64 binary package and an offline Docker image archive are also available from [Releases](https://github.com/zerodenet/zboard/releases).

For Docker deployment, prepare Docker Compose, a database, and a domain with HTTPS. Follow the [first installation guide](https://docs.zerodenet.org/projects/zboard/guides/installation) to configure the service and create your first administrator at `/setup`.

After installation:

1. Add a node and install Zero.
2. Create a protocol service and wait for its configuration to be published successfully.
3. Add the service to a node group and associate that group with a plan.
4. Create a user subscription through the order workflow.
5. Import the user's subscription into a compatible client and check traffic usage in the console.

[ZNet Sink](https://github.com/zerodenet/znet-sink) is the desktop client in the Zero ecosystem. You can also use compatible Clash/Mihomo or sing-box clients with the corresponding subscription format.

## Plugins

The ZBoard core manages nodes, users, subscriptions, basic orders, and traffic usage. Third-party login, online payments, and other business extensions belong in plugins, which operators install and enable as needed.

Install plugins from the marketplace or import a `.zbplugin` package, then manage their configuration, activation, and updates in the console. Available features depend on the plugin and the interfaces exposed by the panel. The payment extension interface is not yet available; payment channels are not built into the core.

[Using plugins](https://docs.zerodenet.org/projects/zboard/plugins/) · [Marketplace and installation](https://docs.zerodenet.org/projects/zboard/plugins/marketplace) · [Plugin development](https://docs.zerodenet.org/projects/zboard/plugins/development)

Deploy a specific [release tag](https://github.com/zerodenet/zboard/releases) and check its release notes for supported features and installation requirements. RC builds are release candidates for deployment validation.

## Documentation

- [First installation](https://docs.zerodenet.org/projects/zboard/guides/installation)
- [Docker storage and backups](deploy/docker/README.md)
- [Node installation and maintenance](https://docs.zerodenet.org/projects/zboard/guides/node-management)
- [Network fronting and shared proxy pools](https://docs.zerodenet.org/projects/zboard/guides/network-fronting)
- [Subscription filtering](https://docs.zerodenet.org/projects/zboard/guides/subscription-filtering)
- [Local development](https://docs.zerodenet.org/projects/zboard/contributing/development) and [contributing](CONTRIBUTING.md)
- [API reference](backend/api/openapi.yaml)

The [documentation index](https://docs.zerodenet.org/projects/zboard/guides/) groups the remaining guides by task.

## Technology

Go, go-zero, and GORM on the backend; Vue 3, Vite, Pinia, and PrimeVue on the frontend. Database drivers are available for MySQL 8 and SQLite. Zero is the node runtime.

## License

ZBoard is licensed under the [Mozilla Public License 2.0](LICENSE).
