# ZBoard 核心边界与复用基线

本文维护长期产品与工程约束。逐轮修复过程、跑测结果和环境记录保存在本地验收产物中。

## 已确定的方向

- 保留现有自用/小规模面板闭环，通过小步修复提高性能、稳定性与交互质量。
- 核心包含用户、登录注册、节点/协议、基础订单、权益、订阅、流量、内核基础限制、文档公告及必要维护。
- 支付尚未接入，未来通过插件补充；当前只渐进整理内部服务边界，不建设插件运行时。
- [XBoard](https://github.com/cedar2025/Xboard) 对照业务流程；[X-Panel](https://github.com/xeefei/X-Panel) 参考基础/增值能力分离；[Typecho 插件实现](https://github.com/typecho/typecho/blob/master/var/Typecho/Plugin.php) 参考精简核心和扩展点。三者都不直接决定 ZBoard 的功能清单或性能结论。
- 现有套餐/SKU、历史订单、权益和节点配置保持兼容，外围能力按真实依赖逐项隔离。未来 ZBoard 插件不能向 Zero Connector 引入支付、套餐或第三方业务语义。

## 已存在且应复用的实现

| 能力 | 当前证据 | 结论 |
| --- | --- | --- |
| 请求取消、旧响应隔离、卸载保护 | [useRemoteResource.ts](../frontend/src/composables/useRemoteResource.ts)、[测试](../frontend/src/composables/useRemoteResource.test.ts) | 已实现；检查页面是否正确接入，不另造框架 |
| 分页/游标表格与生命周期处理 | [useRemoteTable.ts](../frontend/src/composables/useRemoteTable.ts)、[useCursorTable.ts](../frontend/src/composables/useCursorTable.ts)、[生命周期测试](../frontend/src/composables/remoteTableLifecycle.test.ts) | 已实现；逐页验证使用方式 |
| 路由懒加载 | [router/index.ts](../frontend/src/router/index.ts)、[路由测试](../frontend/src/router/routeLoadingPolicy.test.ts) | 已实现；不再列为待建设 |
| 流量分页与区间汇总分离 | [useTrafficUsageTable.ts](../frontend/src/composables/useTrafficUsageTable.ts)、[后端测试](../backend/internal/handler/traffic_usage_statistics_test.go) | 测试已验证无需总计的分页只执行一次 bucket 查询、summary 执行两次查询；不等于已测得生产耗时 |
| 图表独立加载、错误重试、筛选隔离 | [Traffic.test.ts](../frontend/src/views/Traffic.test.ts) | 管理端和用户端复用既有加载与重试行为 |
| 订单状态转移与已支付幂等入口 | [handlers.go](../backend/internal/handler/handlers.go)、[状态测试](../backend/internal/handler/handlers_test.go) | 已有实现；并发和真实权益发放仍需按具体场景验证，不能称为完全缺失 |
| Fair Use 观测 | [FairUse.vue](../frontend/src/views/FairUse.vue) | 已有观测功能；不能误称当前已经执行动态惩罚 |

## 扩展与验证边界

核心独占订单、权益、计量和授权状态转移；外部能力通过明确的命令与查询调用。关键事务不等待可选外部动作，可靠通知根据真实需求设计恢复与幂等语义。

性能问题必须通过测量确认，不能仅凭同步调用或模块依赖判断瓶颈。验收预算见[路线图](roadmap.md)，可复跑方法见[开发指南](development.md#performance-and-stability-verification)。测试通过不等于生产容量、真实节点撤权或长稳已经达标。
