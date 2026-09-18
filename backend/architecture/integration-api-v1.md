# 外部集成 API v1

本文件描述 codex/runtime-jobs 工作树中的接口，尚未部署。当前开放本人范围的 `metering.usage.query`、`commerce.orders.list` 和 `commerce.payments.record`（契约版本 1.0），不继承管理员权限，也不提供数据库或任意订单写入能力。

## 凭据管理

以下入口使用用户登录认证，凭据归当前用户所有：

- `POST /api/v1/account/integrations/credentials`：严格 JSON `{ "name": "支付插件后端", "scopes": ["commerce.orders.list", "commerce.payments.record"], "expires_at": "2026-10-01T00:00:00Z" }`。有效期必须在未来且最多 366 天。每账号最多 20 个有效凭据。成功响应 `data.token` 为仅此次返回的原文；`data.credential` 是可再次查询的元数据。
- `GET /api/v1/account/integrations/credentials?offset=0&limit=50`：返回 `data.items`，每页最多 100 条，包含到期和撤销状态，不返回原文或哈希。
- `DELETE /api/v1/account/integrations/credentials/:id`：撤销本人凭据，重复撤销成功。创建/撤销与审计同事务提交。

客户端应安全保存返回的 `zbi_` 凭据。数据库仅存哈希；丢失后重新创建，不提供找回明文。旧无有效期的 API 凭据不会自动获得新能力权限。

## 发现与调用

使用 `Authorization: Bearer <zbi_凭据>`。用户登录 JWT 不可作为集成凭据使用。

- `GET /api/v1/integrations/capabilities`：只返回当前凭据可见的能力描述，包括版本、输入/输出 schema、范围和限制。每项的 `rate_limit_per_minute` 是该凭据调用此能力的固定分钟窗上限；当前 `metering.usage.query` 为 60。
- `POST /api/v1/integrations/capabilities/metering.usage.query/invoke`：以下 JSON 作为请求体。

```json
{
  "from": "2026-09-01T00:00:00Z",
  "to": "2026-09-02T00:00:00Z",
  "bucket": "hour",
  "limit": 50,
  "include_totals": true
}
```

时间范围为前闭后开，最多 366 天。bucket 为 minute/hour/day；limit 省略默认 50，显式值必须 1–200。可选 subscription_id、node_id、protocol_endpoint_id 均在本人范围内进一步筛选。user_id 只能省略、为零或为本人 ID。未知字段会被拒绝。

响应沿用 APIResponse 信封，`data.items` 为分组记录，`data.statistics` 是可选的显示统计，未请求时为 null；`data.has_more` 表示当前翻页方向还有数据。统计可能来自两秒缓存，页面实时读取，不能把统计视作当前页的事务快照。

下一页携带最后一条记录的游标：`"cursor": {"at":"<record_at>","id":123,"direction":"older"}`。返回较新的页时使用首条记录和 `newer`。游标不会改变授权或时间范围，翻页时保留原筛选条件。

`commerce.orders.list` 只返回凭据所有者的有界订单投影，支持 status、offset、limit，不返回原始支付回调。支付插件自行负责渠道 SDK、结算页、回调地址与签名验证；验证完成后，其后端可调用 `commerce.payments.record` 提交订单、事件/渠道交易号、paid/failed、金额、币种和发生时间。ZBoard 重新校验订单归属、金额、币种、合法状态转换及事件幂等，并在同一事务完成支付事件、订单结算、权益履约和审计。浏览器插件会话不能调用该写能力。

## 失败与运行约束

无效、到期、撤销、越 scope 或越账号请求返回 401；参数错误返回 400；未知能力返回 404；支付事实与当前订单冲突返回 409；超过凭据与能力组合的固定分钟窗上限返回 429，并携带 `Retry-After: 60`；超时返回 504；准入服务不可用返回 503；其他内部失败返回通用 500，不返回数据库错误。能力失败的 `error.code` 只使用 `invalid_argument`、`unauthenticated`、`permission_denied`、`not_found`、`conflict`、`rate_limited`、`unavailable`、`deadline_exceeded`，并通过 `error.retryable` 明确客户端是否可重试；即使可重试，客户端仍须遵守能力幂等契约和 Retry-After。目录和调用每次重验凭据，准入时再次核验凭据、账号、撤销和到期状态；只有通过准入的调用才进入业务处理。调用采用 30 秒协作式超时，请求体最多 64 KiB。创建、目录、列表及调用成功响应标记 no-store。

维护状态和数据库迁移锁沿用系统中间件：迁移锁期间 POST 调用也可能返回 503，即使目标能力是只读查询。当前分钟窗计数通过单条条件更新原子准入；插件页面会话使用进程内同粒度窗口并复核 generation。尚未实现分布式插件配额、跨账号集成授权和真实 MySQL 升级验收；本接口不是 P1/P4 全部完成的声明。

账户页面 `/account/integrations` 已在本地实现，从“账户安全”进入；页面联调与视觉验收尚未完成。
