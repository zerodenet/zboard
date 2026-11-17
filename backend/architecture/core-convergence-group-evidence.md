# P3：套餐接入组替换、当前授权与并发证据

2026-10-20，云端 `/workspace/zboard`，`codex/core-convergence`。本批是 P3 的一个可验证小闭环，不代表整个 P3–P7 完成。执行源为基于 `2073edcee117112d9cf71d52fddb86fe3e973475` 的工作区变更；最终提交、Draft PR #78 的整体验证由主代理集成记录。本页与 [确认决策](core-convergence-decisions.md)、[实施计划](core-convergence-plan.md) 交叉引用，不以历史计划作为实现证据。机器可读结果见 [本批验证记录](validation/2026-10-20-plan-group-sync.json)。

## 问题与实际行为

此前更新套餐 `node_group_id` 不会把该套餐历史订阅迁到当前接入组。订阅可能保留旧组授权；此外，付款在等待 Plan 锁前建立的 MySQL repeatable-read 快照，可能漏掉刚提交的新组成员、端点及前置入口。外层锁不能使嵌套 UNION 派生表成为当前读。

现在仍只有 `commercestore.PlanUpdate.Update` 一条套餐写入路径。同一事务更新套餐修订、符合条件的历史订阅组、移除旧范围凭据的意图、旧/新节点发布意图与审计。组替换自动应用，无订阅例外；超过 1000 份整体拒绝，不部分推进。本批不改变 `group.IsEnabled` 既有禁新售语义。

订阅只改变接入组及更新时间；已用量、额度、起止时间、重置日、重置策略、生命周期、订阅身份、权益代次、锁价及历史订单均不重写。恢复窗口内的订阅可跟随组策略，但不会因此恢复服务；已结束、退订、超过恢复窗口或已耗尽的固定总额度实例不会被重启。周期耗尽与业务终止仍分别处理。

共享端点保留原凭据、密钥与有效交付确认。移出的端点撤权并保留旧节点发布意图；新端点由既有正常 reconciliation 准备带 `DeliveryRequired` 的绑定，实际确认前交付状态为 pending。旧确认、旧 reconciliation 快照、旧冻结回包都不能重新授权被移出的端点，也不能替代 A→B→A 后的新交付确认。

旧 pending 新购、续费、换购保留其原支付承诺，用最终锁定套餐的当前组履约，不改历史购买/换购快照或金额。无实际资源竞争时，旧 RR 快照遇到后提交的新组与成员仍可直接成功；测试还核对付款自身生成的新节点/前置入口发布意图，不能仅依赖此前组变更留下的队列。

## 当前读与事务锁边界

`networkstore.CurrentCredentialEndpointIDs` 分别对真实直接成员及前置入口成员做当前读；实际端点主键读取也为当前读。撤权先取得该当前 ID 集合，再对凭据做明确的 `NOT IN(ids)`。空集合撤销所有旧绑定，不生成 `NOT IN(NULL)`。共享端点不能先被旧快照误撤权，再依赖后台任务补救。

本批新增的 MySQL 资源 SHARE 锁均使用 NOWAIT。只有驱动错误 3572 转为 `network.ErrResourcesBusy`；1213、1205、约束失败等保留原失败。SQLite 沿用已有写事务。业务保留原事务边界，资源忙时整笔回滚，既不是支付事实不可信，也不是已应用成功。既有 HTTP/宿主临时不可用契约继续复用：HTTP 503、`unavailable`、`retryable=true`、`Retry-After: 1`。浏览器不自动重放写操作。

履约在旧凭据到期、恢复或关闭前，准备实际目标组的当前身份。加购/单独重置使用目标订阅本身的组；其他授予使用实际 GrantPolicy 的当前组。确切端点/队列的 Node FK 父身份亦以 NOWAIT 保护，避免 INSERT 的隐含父锁等待形成反向环，不锁无关节点、不改 Node 状态。

`EnqueueNodePublication` 不在已持凭据锁后重新锁 Endpoint；它保留既有节点端点身份并补上显式受影响 ID，随后当前读取前置路由。订阅路由将同一节点的全部当前端点身份一起传入，不因 node 去重丢失第二个新端点的前置入口；`endpointID=0` 的节点级发布保留多端点/多路由覆盖。队列租约、未知结果核验和旧确认所有权逻辑未修改。

共用立即锁/FK 帮助实现位于已有 `credentialstore/current_resource_locks.go`，networkstore 只有薄委托。这样保留已有 networkstore→meteringstore 依赖，不引入反向导入环。计量仅在共同的 `ResolveCompletionCredential` 中，在凭据锁之前保护本次实际来源 Node 和候选 Endpoint 的 FK 父；不扫描组、不按 active 过滤历史事实、不改代次、归属、去重、倍率或计费。批内多事件不需要预锁全组；每个新增资源锁立即成功或整笔返回可重试冲突。

## 验证与保留的失败记录

所有命令在 `backend` 执行，Go 1.26.8，`GOMAXPROCS=1`、`-p 1`；MySQL 8.4 使用本机隔离实例与随机测试数据库，未使用生产库。测试耗时不是接口延迟或吞吐量承诺。

```sh
ZBOARD_TEST_MYSQL_DSN='<isolated test DSN>' GOMAXPROCS=1 \
  go test -p 1 ./internal/handler -run '^TestMySQLPlanGroup' \
  -count=1 -timeout 4m
```

最终完整专项 **PASS 70.762s**，日志 `/tmp/zboard-plan-group-mysql-final.log`。覆盖正常组替换、整事务回滚、1001 上限、生命周期边界、旧 pending 三类履约、旧 RR 下新组/多端点/多前置入口的当前读取、后补共享成员的已确认授权保留、旧/新配置确认隔离。并发通过 `performance_schema.data_lock_waits` 证明端点编辑实际等待 Node 行；组撤权、共享端点换购/恢复和端点编辑串行完成，不放行死锁。

真实资源忙专项在管理员端点编辑仍持端点/凭据锁并准备移除成员时调用钱包付款及支付事实入口。失败时订单、预留、账本、支付事件/收据、权益及发布意图完整不变；HTTP 钱包入口返回可重试 503。编辑完成后同一旧订单/支付事件可重试，第二次重放不新增财务或交付副作用。另有只持 Node X、不持 Endpoint X 的 FK 父冲突专项。

缓冲计量专项在真实 Endpoint X 锁下调用实际 `consumeZeroEventBatch`。资源忙返回零进度，spool checkpoint 不动，计费事务与流量/累计游标回滚；解除锁后同一事件推进一次，再模拟 checkpoint ACK 丢失重放，仍仅一条计费事实和一次 30-byte 扣量。这里使用测试 EventSpool 观察提交边界，不冒充本批新增了文件缓冲耐久性验收。

保留中间失败，未延长超时或放宽断言：

- 首轮完整专项 **FAIL 69.803s**，`/tmp/zboard-plan-group-mysql.log`：原最小 outbox fixture 缺合法公开地址，真实端点编辑在进入锁前被校验拒绝；1001 订阅 fixture 空 JSON 被 MySQL 拒绝；该首轮 binary 尚无新增 Completion FK 即时父锁，缓冲入口在 FK 上出现 context deadline。后者是实际缺口，已在真实共同计量入口修复。
- 首次窄重编译曾遇 networkstore↔meteringstore 导入环，未执行数据库；共用 helper 移入已有 credentialstore 后消环。
- 中间窄轮 **FAIL 36.210s**，`/tmp/zboard-plan-group-mysql-locks.log`：1001 fixture 尚缺真实 PlanSKU FK；组撤权竞争测试错误等待 credential 表，而实库已先在 Node 行等待，导致 5 秒上下文到期。补全真实 fixture 并按实际 Node 行验证，两操作都必须成功，未把 context deadline、1213 或任意错误当成功。该轮付款/支付事实、HTTP、buffer 重放、换购/恢复共享端点与 Node FK 专项已通过；最终证据以上述完整通过轮为准。

最终 SQLite/纯 Go 组合回归：

```sh
GOMAXPROCS=1 go test -p 1 \
  ./internal/handler ./internal/adapters/persistence/networkstore \
  ./internal/adapters/persistence/credentialstore ./internal/adapters/persistence/meteringstore \
  -run '^(TestPlanGroup|TestFullReplacement|TestApprovedRenewal|TestPlanQuota|TestResourceContention|TestCurrentResourceRead|TestCompletion|TestCredentialReconciliationSQLite|TestZeroAccounting|TestZeroCompletion|TestAccountingBatch|TestConsumerCheckpoint|TestNodeCredential|TestGroupCredential|TestTrafficReset|TestSubscriptionQuota)' \
  -count=1 -timeout 4m
```

**PASS**：handler 10.205s、networkstore 0.007s；credentialstore 无测试文件、meteringstore 此模式无匹配测试，二者编译通过。实际 Completion/Batch 行为由 handler 的组合与上述 MySQL 专项验证。日志 `/tmp/zboard-plan-group-final-sqlite.log`。没有增加 CI 排除项。

## 迁移、兼容、回滚与未验证项

没有新 schema、迁移编号、队列种类或财务写入路径。使用现有套餐/订阅组、凭据状态、发布队列和审计记录。二进制回滚不等于撤销已提交的组策略；要恢复业务范围，应通过新的管理员套餐修订与正常交付流程，保留审计及已确认/待确认记录，不能删队列或伪造确认。

本批没有修改内核、客户端、外部插件、生产配置或生产数据；没有合并、发布或部署。配置完成测试为冻结结果的持久化模拟，连接撤权测试只验证核心决策及回调边界，不证明真实远端断连。仍需最终集成提交的完整后端/双库/前端/边界 CI，以及真实部署版本的原生发布与断连验收、容量测量；本批不宣称吞吐量改善或零超用。超过 1000 份的组同步仍整体拒绝，异步大规模迁移不在本批实现。
