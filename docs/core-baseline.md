# ZBoard 核心边界与现状差异

更新：2026-09-06。依据提交 `ee3542e` 及当前工作区的代码、测试结果；已实现能力不再作为建设待办。本文不代表全项目审计已经完成。

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
| 图表独立加载、错误重试、筛选隔离 | [Traffic.test.ts](../frontend/src/views/Traffic.test.ts) | 管理端和用户端已有 27 项测试覆盖，本轮通过 |
| 订单状态转移与已支付幂等入口 | [handlers.go](../backend/internal/handler/handlers.go)、[状态测试](../backend/internal/handler/handlers_test.go) | 已有实现；并发和真实权益发放仍需按具体场景验证，不能称为完全缺失 |
| Fair Use 观测 | [FairUse.vue](../frontend/src/views/FairUse.vue) | 已有观测功能；不能误称当前已经执行动态惩罚 |

## 第一轮：详情请求生命周期

涉及 [Users.vue](../frontend/src/views/Users.vue)、[Subscriptions.vue](../frontend/src/views/Subscriptions.vue)、[Orders.vue](../frontend/src/views/Orders.vue)。

| 触发条件 | 修复前 | 修复后 |
| --- | --- | --- |
| 从详情 A 切到 B，A 的旧响应仍然完成 | 直接赋值可能覆盖 B；旧请求错误也可能显示在 B 上 | 使用已有 useRemoteResource，只允许当前请求更新数据和错误 |
| 关闭后重新打开同一个 ID，旧请求才结束 | 按 ID 判断 finally 会错误结束新请求的 loading，并可能显示旧数据 | 请求代次隔离，等待本次打开对应的请求 |
| 详情请求未完成时离开页面 | 页面局部 AbortController 没有卸载清理 | 随组件作用域取消详情请求 |
| 订单详情已返回，支付事件接口缓慢 | detailLoading 等待支付事件，主详情和操作区持续不可见 | 详情独立显示，事件列表独立加载；无新增请求 |
| 支付事件读取中关闭订单抽屉 | 只清空事件数组，未取消请求或失效旧结果 | 重置已有表格资源，同时取消请求、清空总数和错误状态 |
| 订单详情尚未返回时，通过 URL 切换支付事件页 | 分页状态已变更，但请求被 detailLoading 阻止，继续显示旧页结果 | 事件分页独立加载，不重新请求订单详情 |

回归见 [adminDetailLifecycle.test.ts](../frontend/src/views/adminDetailLifecycle.test.ts)：使用真实 Vue Router 与挂载页面，控制响应顺序，覆盖 3 个页面及独立事件列表，共 15 项。传输 mock 特意允许取消后的响应继续完成，以验证数据提交边界；这不是生产网络故障率测量。

此次复用既有公共实现，没有增加新的加载抽象。订单主详情从“等待详情与事件两者都结束”改为“详情返回即可显示”，未提供毫秒级性能收益承诺。

## 第二轮：规则文件安全、表单反馈与局部性能

| 已复现的问题 | 本次处理 | 回归证据 |
| --- | --- | --- |
| 创建规则集时 tag 重复，数据库拒绝插入，但失败清理仍删除该 tag 对应的已有源文件与编译产物 | 仅在本次事务成功插入新记录后执行新建文件清理；保留新建事务失败的清理行为 | [managed_rule_create_test.go](../backend/internal/handler/managed_rule_create_test.go)：真实 SQLite、临时目录，覆盖重复创建和审计写入失败回滚 |
| 规则集草稿未保护路由离开/浏览器关闭；保存读取了错误的后端字段错误层级 | 复用 useFormErrors/useUnsavedChangesGuard，映射 versioned error.fields，定位错误输入、编辑后清除错误，保存失败保留草稿 | [SubscriptionRuleSets.test.ts](../frontend/src/views/SubscriptionRuleSets.test.ts)：4 项页面行为测试 |
| 供应商验证失败后立即刷新列表，刷新函数清空了失败提示 | 刷新状态后保留原始验证错误；多操作统一使用现有 RowActions | [Providers.test.ts](../frontend/src/views/Providers.test.ts)：新增验证失败提示回归，保留删除保护测试 |
| 首页推荐请求在页面卸载后仍继续；固定文案测试未验证真实行为 | 复用可取消资源，保留 3 条推荐上限和静态内容；用真实路由行为替换过期文案匹配 | [Home.test.ts](../frontend/src/views/Home.test.ts)：推荐导航、失败降级、卸载取消 |
| 套餐/规格表单缺少持久错误摘要；样式引用了未定义的间距、背景和阴影变量 | 补齐表单内摘要；补齐间距 token、复用已有背景/阴影 token，设置预览颜色纳入主题文件 | 通用表单、SKU 模型和主题检查 |

规则集重复创建问题在修复前的回归中明确失败：数据库记录仍存在，而源文件读取返回 `record not found`。相关测试使用已有的模拟 ZRS 编译器，因此验证的是数据库与文件清理语义，不代表真实外部编译器互操作已经验证。

内联规则保存原先在校验、保存准备两个阶段重复解析、规范化和排序。现在准备函数返回已验证的规则文档，持久化复用该结果；远端导入语义保持不变。[基准与规范化测试](../backend/internal/handler/managed_rule_preparation_test.go) 使用 10,000 条不同域名规则，测量解析到规范 JSON 编码的过程：

| 指标（3 次采样中位数） | 修改前 | 修改后 |
| --- | --- | --- |
| 分配次数/操作 | 60,150 | 30,069 |
| 分配字节/操作 | 15,940,281 | 8,009,718 |
| 耗时/操作 | 52.45 ms | 27.92 ms |

环境：Go 1.26.5、darwin/amd64、Intel i7-7920HQ、`GOMAXPROCS=1`。命令：`go test ./internal/handler -run '^$' -bench '^BenchmarkManagedRulePreparation$' -benchtime=2s -count=3`。修改前使用 300ms 采样；修改后短采样波动较大，延长至 2s。分配成本约减半；耗时是本机局部观测，未涵盖数据库、磁盘持久化、编译器和 HTTP 请求全程，不能外推为面板吞吐收益。

第一轮留下的 6 项失败已逐项处理，其中有实际缺陷，也有测试契约问题：动态表单审计取消固定数量断言；局部绑定的 `--series-color` 不再误判为缺失全局变量；管理端行操作检查限定管理页面与共享组件；首页不再锁定过期文案。全量复测还发现 SKU 测试禁止内联错误摘要，与通用表单要求冲突，已统一为即时通知和持久表单摘要同时保留。测试通过数量不等于修复缺陷数量。

## 第三轮：订单确认与权益发放

真实 SQLite 与完整迁移下复现并修复：

- 同用户再次新购相同 SKU，原实现把新订单并入已有订阅、延长时间并增加额度。现在明确为 `new` 且未指定目标的订单创建独立订阅，显式续费继续使用指定目标。
- 两张待确认新购订单在容量尚未占用时创建，第一张确认后，第二张原本绕过容量复核并变成续费。现在确认阶段复用统一容量检查，容量不足返回原有 `plan_subscription_limit_reached` / HTTP 409，订单保持待确认。
- 管理员记录取消结果时，原实现仅更新状态，未写入取消时间。现在状态与取消时间一并提交。

[order_result.go](../backend/internal/handler/order_result.go) 收拢手工确认与结果记录共用的锁定、发放、订单更新和审计事务，接收请求上下文并返回明确的业务错误与实际发放结果。HTTP 层继续负责鉴权和响应；既有路由保留，订单包装器不再缓存、解析另一个 handler 的 JSON 来识别容量错误。这是内部调用边界整理，不是在线支付接入或插件运行时。

手工确认移除了事务外的重复订单读取；明确新购不再查询已有同 SKU 订阅；重复确认已支付订单不再触发配置发布。回归通过数据库查询钩子验证重复确认没有读取发布端点。这里记录减少的工作，不宣称已测得生产响应时间或吞吐提升。

回归见 [order_fulfillment_test.go](../backend/internal/handler/order_fulfillment_test.go) 与 [order_result_test.go](../backend/internal/handler/order_result_test.go)，还覆盖显式只延长有效期的续费、审计失败时订单/订阅/额度事件回滚、重复确认不重发权益及不重写发放时间、已支付订单拒绝后续失败/取消结果、已取消请求上下文不提交变更。尚未验证 MySQL 并发竞争或真实节点交付。

## 第四轮：发布持久化与故障恢复

- 发布状态改存 `node_config_publishes`，每节点最多一条待办，记录版本、领取标记、重试次数和下次尝试时间。同节点多次变更合并，执行期间的新版本不会被旧结果删除。
- 订单发放、凭据到期、流量耗尽、配额任务调整、协议端点直接变更、内核操作完成状态与相应发布请求在同一数据库事务提交；队列写入失败时业务事务回滚，避免提交后入队前的丢失窗口。流量耗尽会为订阅所在节点组的全部相关节点入队。
- 启动主动扫描未完成任务，四个工作线程处理，一个每 5 秒触发的定时器负责空闲轮询。失败从 5 秒开始退避，最长间隔 5 分钟，保留重试；宕机时遗留的领取标记在 3 分钟租约到期后可恢复。
- 领取与完成操作均校验所有权，旧执行结果不能确认新线程领取的任务；领取时再次检查下次尝试时间，避免候选读取和实际领取之间的退避竞争。
- 数据库升级遵循现有开发基线政策：新增表进入嵌入 SQL、SQLite 模型清单及数据库搬迁清单；已存在的开发数据库增量创建队列表。

实现见 [队列存储](../backend/internal/handler/node_publish_queue.go)、[工作线程](../backend/internal/handler/node_publish_worker.go)；恢复、并发领取、事务回滚和迁移测试见 [队列回归](../backend/internal/handler/node_publish_queue_test.go)、[业务事务回归](../backend/internal/handler/node_publish_transaction_test.go)。恢复语义与运维检查见 [节点配置交付](node-config-delivery.md)。

## 第五轮：订阅撤权路径补漏

- 读取订阅时触发的过期处理，原先直接更新订阅和凭据状态，没有生成发布请求。后台到期扫描随后会跳过已失效订阅，导致节点同步遗漏。现在读取路径的状态更新、凭据失效和发布请求在同一事务提交；已有事务的订单/流量调用复用事务内实现，不额外嵌套事务。
- 套餐切换导致节点组变化时，原先只为新组准备凭据并发布。现在撤销不属于新组的旧凭据，同时为旧节点生成发布请求；新组共用的端点保留凭据，切换失败则原订阅、凭据和订单一起回滚。
- 撤权查询只读取凭据 ID、节点和端点 ID，不读取密钥；节点入队按 ID 排序去重。此处只陈述处理方式，未把它折算为生产性能收益。

实现见 [subscription_revocation.go](../backend/internal/handler/subscription_revocation.go)，[5 项回归](../backend/internal/handler/subscription_revocation_test.go) 覆盖读取触发过期、入队失败回滚、跨组切换、切换失败回滚和共用端点保留。问题在修复前均有对应失败证据；真实节点上的撤权时延尚未测量。

## 第六轮：SSH 取消与 MySQL 并发实测

- 发布任务的 2 分钟超时原先没有传入 SSH 握手和远端命令。本地真实 TCP/SSH 服务器复现了握手、会话建立、命令三种卡死，取消任务后执行器仍不返回。现在完整握手限时 12 秒，发布和探测连接响应任务取消，关闭连接后保留队列重试。内核健康探测也传递期限，失败诊断最多额外等待 10 秒。
- MySQL 8.0.29 实测发现，管理员同时确认自己和其他用户的同套餐订单时，买家独占锁、套餐锁与审计外键的操作者共享锁形成死锁。现在在套餐锁之前，按用户 ID 顺序取得买家独占锁和操作者共享锁；共享操作者锁允许不同买家的确认并行，不使用全局互斥或将审计移出事务。
- 新增可选真实 MySQL 测试，每次自行创建和清理随机测试库。覆盖旧库补表、16 路并发队列、租约/确认恢复、8 路重复订单确认、发布失败时订单/撤权回滚和并发容量限制。SQLite 测试夹具的空 JSON 已补为合法 JSON，此项是测试数据修正，不计为产品缺陷。

实现见 [ssh_execution.go](../backend/internal/handler/ssh_execution.go)、[order_result.go](../backend/internal/handler/order_result.go)。复跑入口及真实节点验证边界见 [节点发布说明](node-config-delivery.md)。

## 仍需核查的问题

以下内部边界仍值得继续核查，但尚未证明是性能瓶颈：

- [zero_event_runtime.go](../backend/internal/handler/zero_event_runtime.go) 在核心计量后同步执行 Fair Use coverage 投影，错误已隔离。需要测量这部分成本和关闭路径，不能凭调用存在就声称吞吐低。

扩展边界仅保留必要约束：核心独占订单/权益/计量/授权状态转移，外部能力经明确的命令与查询调用；关键事务不等待可选外部动作，可靠通知在出现真实需求时设计恢复与幂等语义。

## 验证范围

- 第一轮全量前端为 441 项，435 项通过、6 项存量失败；新增详情回归 15 项属于同一组生命周期问题。
- 第二轮后端 `go test ./...`、`go vet ./...` 通过；前端全量 123 个文件、448 项测试全部通过，类型检查和生产构建通过。
- 第三轮后端 `go test ./...`、`go vet ./...` 通过；未修改前端，未重复执行前端检查。
- 第四轮后端 `go test ./...`、`go vet ./...` 通过；队列恢复、业务事务及独立工作线程竞争/停机测试通过 `-race` 检查。未修改前端。该轮 Docker daemon 不可用，MySQL 和本地 SSH 故障验证在第六轮补齐。
- 第五轮后端 `go test ./...`、`go vet ./...` 通过，新增 5 项订阅撤权回归全部通过；未修改前端。
- 第六轮设置真实 MySQL 测试 DSN 后，后端 `go test ./...`、`go vet ./...` 通过；MySQL 并发、SSH 取消及队列/事务相关测试通过 `-race`。死锁场景修复后连续 5 次通过，随后全量和竞态回归也通过。未修改前端。
- 性能预算继续使用 [路线图](roadmap.md) 的约束；1C1G 压测、生产 SQL profiling、真实浏览器/节点闭环和 24 小时长稳本轮未执行，不能据此宣称整体性能或底层稳定性已达标。
