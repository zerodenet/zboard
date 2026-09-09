# ZBoard

[English](README.md) | [简体中文](README.zh-CN.md)

**A proxy management panel for personal and small-scale use.**

ZBoard connects users, nodes, subscriptions, basic orders, and traffic accounting in a complete management workflow, alongside login, registration, public documents, announcements, and essential maintenance.

The current priority is to harden the existing workflow, fix frontend and backend defects, and reduce resource use. Online payment is not integrated; it is a future plugin capability. Internal service boundaries are being prepared before a plugin runtime is built.

> ZBoard is under active development. The current development baseline is `v0.0.1`, with `v0.1.0` planned as the first public release.

## Core management workflow

```text
VPS infrastructure
        ↓
Protocol services
        ↓
Node groups
        ↓
Plans / SKUs
        ↓
Orders / Subscriptions
        ↓
Client configuration delivery
        ↓
Traffic accounting
```

## Core capabilities

### Infrastructure management

- Manage VPS assets, SSH credentials, host trust, and node status.
- Install, validate, upgrade, and rollback Zero runtime components.
- Publish node configurations and track operational results.

### Protocol services

- Support VLESS, VMess, Shadowsocks, Trojan, and Hysteria2.
- Separate protocol services from physical nodes.
- Reuse, migrate, and organize services through node groups.

### Subscription delivery

- Generate client-native configurations for ZNet Sink, Clash/Mihomo, and sing-box.
- Manage templates, rules, policy groups, outbound targets, and node filters.
- Validate configurations before delivery.
- Rotate and revoke subscription credentials securely.

### Users, orders, and entitlements

- Manage users, plans, SKUs, orders, subscriptions, renewals, and quotas.
- Preserve order and entitlement snapshots.
- Activate entitlements through administrator order confirmation without an online payment plugin.
- Provide user-facing subscription and usage information.

### Traffic and operations

- Receive authenticated traffic events from nodes.
- Attribute usage to subscriptions.
- Provide operational logs, tasks, audit records, backups, upgrades, and rollback workflows.

## Resource model

```text
Node asset → Protocol service → Node group → Plan / SKU → Order → Subscription
```

ZBoard separates node resources, order records, and entitlements so that node changes do not rewrite historical orders. Existing plan/SKU models remain compatible during hardening.

## Current direction

- Preserve working core flows and improve reliability and usability through small, verified changes.
- Measure query latency, memory, concurrency, and end-to-end correctness.
- Use XBoard to compare core workflows, X-Panel to inform basic/add-on scope, and Typecho to inform a lean core with explicit extension points.
- Gradually isolate complex commercial and policy capabilities for future plugins; implementing a plugin runtime is outside the current phase.

See the [core and hardening baseline](docs/core-baseline.md) for scope, sources, and acceptance criteria. These are implementation goals, not claims that existing optional features have been removed or performance targets met.

## Technology

| Component | Technology |
| --- | --- |
| Backend | Go, go-zero, GORM |
| Frontend | Vue 3, Vite, Pinia, PrimeVue |
| Data | MySQL 8 / SQLite |
| Runtime | Zero |
| API | RESTful `/api/v1`, OpenAPI |

## Documentation

Development, deployment, API references, and operational guides are maintained in the [documentation](docs/).

## License

ZBoard is licensed under the [Mozilla Public License 2.0](LICENSE).

Plugin extensions

- Install signed frontend and administrator extensions from a configured marketplace or an offline `.zbplugin` file.
- Enable, disable, configure and inspect plugins without restarting ZBoard. Core business changes remain owned by ZBoard.
- See [plugin setup and development](docs/plugins.md) for trusted publishers, package signing and current capability limits.
