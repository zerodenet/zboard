# 插件账户页面调用核心能力

本地实现，尚未部署或进行真实插件包端到端验收。

插件声明 `zboard.ui.page.v1` 以及所需的 `zboard.metering.read.v1` 或 `zboard.commerce.orders.read.v1`，并贡献 account/business 页面后，可通过已有 UI bridge 请求以下类型：

- `capabilities.list`：读取该插件页面当前允许调用的目录。
- `capabilities.invoke`：提供获准的 `operation` 和 `input` 对象；当前插件页面只可调用 `metering.usage.query` 或本人范围的 `commerce.orders.list`。

示例发送消息（沿用既有 frame 来源校验、bridge_token 和 request_id）：

```js
parent.postMessage({
  source: 'zboard-plugin-ui',
  bridge_token: bridgeToken,
  request_id: 'usage-1',
  type: 'capabilities.invoke',
  operation: 'metering.usage.query',
  input: {
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-02T00:00:00Z',
    bucket: 'hour',
    limit: 50
  }
}, '*')
```

插件沿用宿主桥接响应，不能获取用户登录令牌。宿主只转发 operation/input，不采信消息顶层 plugin_id、user_id、administrative 等权限字段；输入中的其他用户 ID 也会被能力入口拒绝。

每次调用检查：当前会话用户、account/business 用途、实例 generation、插件启用与准入记录、声明能力和当前账号状态。授权锁只覆盖验证，不覆盖计量查询执行。目录隐藏未授权契约，调用未授权契约返回 403；暂时不可用返回 503。停用再启用后需要创建新会话，旧会话不会恢复权限。

当前不授予管理页面、公共页面、身份 slot 或后台原生进程此能力，也不继承用户管理员角色。后台原生进程主体、受限系统授权及任务关联调用仍待实现；现有私有存储 Unix socket API 保持原范围。

支付插件不能让宿主创建 checkout、承接渠道回调或执行验签。上述流程完全属于插件；需要落账时，插件后端使用用户显式签发且包含 `commerce.payments.record` scope 的外部集成凭据提交已验证事实。该命令不会暴露给 iframe/browser plugin_session。


> 2026-10-19：本轮产品决策与 P0–P7 验收以 [核心收敛决策](core-convergence-decisions.md) 和 [实现证据/差距](core-convergence-plan.md) 为准。本文历史阶段与完成记录保留，不代替当前实现验证。


## 支付信任边界修正（2026-10-19）

历史自助集成凭据的 `commerce.payments.record` scope 不能证明渠道收款。
现在仅当前有效管理员可签发并调用其本人账户的支付事实集成凭据；普通账户的旧支付 scope 也拒绝执行。
每次发现、调用准入及最终订单事务均重验权限，不继承其他管理能力，也不扩大到账户外订单。
普通用户支付使用已准入原生插件及订单绑定 host grant：浏览器仅触发 checkout，渠道核验由插件后端完成，核心仍核验金额、币种、订单和幂等。
原生插件可处理普通用户订单，停用/撤权/实例退出后不能继续接收事实；本修正不改变原生 SDK。
