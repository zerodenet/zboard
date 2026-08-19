# ZBoard

[English](README.md) | [简体中文](README.zh-CN.md)

**An all-in-one management platform for proxy service providers.**

ZBoard connects VPS infrastructure, protocol nodes, subscription delivery, users, plans, orders, traffic accounting, and daily operations in one system.

It is not only a subscription panel. ZBoard manages the complete lifecycle of a proxy service platform: from VPS onboarding and protocol service management to product sales, subscription delivery, traffic settlement, and operational auditing.

> ZBoard is under active development. The current development baseline is `v0.0.1`, with `v0.1.0` planned as the first public release.

## Complete service lifecycle

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

### Business operations

- Manage users, plans, SKUs, orders, subscriptions, renewals, and quotas.
- Preserve order and entitlement snapshots.
- Provide user-facing subscription and usage information.

### Traffic and operations

- Receive authenticated traffic events from nodes.
- Attribute usage to subscriptions.
- Provide operational logs, tasks, audit records, backups, upgrades, and rollback workflows.

## Resource model

```text
Node asset → Protocol service → Node group → Plan / SKU → Order → Subscription
```

ZBoard separates infrastructure resources from commercial resources, allowing nodes and services to evolve without changing existing customer entitlements.

## Why ZBoard

Traditional panels usually focus on subscriptions and products while leaving VPS management, protocol lifecycle, and operational workflows to external tools.

ZBoard brings infrastructure, service delivery, and commercial operations together:

| Traditional approach | ZBoard |
| --- | --- |
| Manage nodes separately from products | Connect infrastructure and business resources |
| Bind subscriptions directly to servers | Use reusable services and node groups |
| Generate static subscriptions | Deliver validated client configurations |
| Maintain isolated traffic counters | Associate runtime events with subscriptions |

## Technology

| Component | Technology |
| --- | --- |
| Backend | Go, go-zero, GORM |
| Frontend | Vue 3, Vite, Pinia, PrimeVue |
| Data | MySQL 8, Redis |
| Runtime | Zero |
| API | RESTful `/api/v1`, OpenAPI |

## Documentation

Development, deployment, API references, and operational guides are maintained in the [documentation](docs/).

## License

ZBoard is licensed under the [Mozilla Public License 2.0](LICENSE).
