# 核心渐进收敛：实施与证据

2026-10-19。本轮 [确认决策](core-convergence-decisions.md) 是权威需求。起点 `7c7283fb34840883fccaba1de6e0a98edb24dfb1`，分支 `codex/core-convergence`。历史 `capability-plan.md`、`refactor-progress.md`、`metering-statistics-performance.md`、`plugin-capabilities-v1.md`、`plugin-capability-bridge-v1.md`、`integration-api-v1.md` 与 `capability-ownership.json` 保留；其中旧阶段、发布断言、工单迁出方向和支付集成自签授权不是本轮验收证据。

## 状态口径

只用：**已实现且验证**（明确范围与本轮命令）、**实现未验证**（当前代码存在，本轮未跑）、**部分实现**、**未实现**、**外部能力不足**。生产和外部端到端未验收不能由单元/沙箱测试替代。下表是 P0 代码审计起始状态；后续批次在末节追加测试结果，不修改历史记录成虚假通过。

## 需求—实现—差距表

代码均相对 backend/，除明确 frontend/。每行覆盖对应决策的所有子项，未列为已验证的子项均仍需后续验收。

| 决策 | 状态 | 当前代码/数据/接口/任务证据 | 缺口与阶段 |
|---|---|---|---|
| D01 基线/近期修复 | 已实现且验证 | fetch、HEAD/origin/develop、干净状态；0035/0036 迁移；49b622e/7c7283f 均祖先 | PR/后续 CI 见批次记录，P7 再核 develop 增量 |
| D02 核心/扩展/统一入口 | 部分实现 | capabilities、application、adapters/persistence；architecture/boundaries_test.go；experience 工单 | 余额/报价扩展未实现；旧工单迁出计划失效；P2/P5 |
| D03 内核适配/探测 | 部分实现 | adapters/zero；capabilities/network；kernel_reconciliation；native readiness/probe | 通用能力矩阵与所有管理页解释待实测；无其他内核适配器；P3 |
| D04 失联期限 | 外部能力不足 | Connector/心跳、native 管理探测不构成授权到期执行证明 | 未验证公开内核具可靠租约到期执行，不能开放可保存的假设置；P3 记录限制 |
| D05 未知发布/队列边界 | 实现未验证 | zero/configuration_reconciliation.go，networkstore/configuration_publication*.go；node_publish_mysql_test.go | 本轮 SQLite/MySQL 故障重放、队列所有权再验；P1/P3 |
| D06 独立订阅/端点/组 | 部分实现 | model.Subscription/ProtocolCredential/NodeGroupEndpoint；entitlementstore/fulfillment、revocation；networkstore/protocol_endpoint_mutations | 无聚合链接需求；plan 变组对存量同步仍缺，组增删与交付完整链待验；P1/P3 |
| D07 套餐同步 | 未实现 | commercestore/plan_update.go 只更新目录、审计 | 可选历史额度同步及事务内撤权/交付；P2 |
| D07 基础锁价 | 部分实现 | Subscription.RenewalPriceMinor、订单 Amount/Discount/Payable/Paid | prepareOrder 仍用当前 SKU 价；setPaid 仍用当前 SKU 设锁价；P2 优先修 |
| D08 优惠扩展 | 未实现 | 现有 checkout 插件及订单金额列 | 无优惠约定/版本/不可用状态，不能自动涨价扣款；P2 |
| D08 独立周期 | 部分实现 | BillingUnit/Value 与 ResetPolicy/NextResetAt | 无真实付费周期金额分摊快照；P2 |
| D09 换购残值/新期限/余款 | 部分实现 | commerce.RemainingCredit，commercestore/order_quote，entitlements.ApplyGrant | 现为总实付×时间/当前额度比例、抵扣封顶新价，保旧期限/已用；三个确认算例不满足；P2 需替换同一写路径 |
| D09 报价并发 | 实现未验证 | ChangeSnapshot、QuoteFingerprint、validateTargetQuote 校验 source/quota event/计费状态 | 扩展到真实周期/余额/退款版本后再验；P2 |
| D10 退订/价值防重复 | 未实现 | 只有管理员 subscription mutation cancel，无用户余额残值事务 | 需结算身份、剩余可退与撤权同事务；P2/P5 |
| D10 舍入/旧数据 | 部分实现 | commerce.proportionalValue 大整数半入、秒级；commercestore/sourceValue | 未定义非整除周期分摊余分、旧数据真实快照缺失处理；不伪造，P2 缺失决策单独确认 |
| D11 支付渠道/可信事实 | 部分实现 | plugins/checkout，application/plugin_native_business，commerce_catalog；payment_facts 原子履约 | 普通用户可自签包含 payments.record 的 integration token，绕过渠道核验；P1 优先关闭，保留可信 native 插件路径 |
| D11 支付幂等/恢复 | 实现未验证 | PaymentEvent(provider,event_id)，order transition，同事务权益+outbox | 迟到支付目前冲突返回但未建立资金后续处理账；P1 故障回滚、P2 入账与履约分离 |
| D12 余额/充值/混合支付 | 未实现 | model 无金额钱包/预留/资金账本（QuotaEvent 的 balance 为流量） | 建整数余额/预留/账本、资金来源与订单关联及释放竞态；P2 |
| D13 消费退款/人工扣减 | 未实现 | Order.RefundAmount 仅数据列，无受控余额流程 | 不提供提现；工单关联、预留保护、累计可退；P2/P5 |
| D14 业务成功后后台交付 | 实现未验证 | commercestore/payment_facts/order_settlement + networkstore/publication_intents 同库 | 正常/断网/回放端到端证据；P1 |
| D14 提前续费/重置 | 部分实现 | entitlements/grant/renewal；traffic_reset，订单快照 | 旧 extend_and_add_quota 可立即加额度；确认新规则需新单兼容策略；P2 |
| D15 到期/耗尽/恢复 | 部分实现 | lifecycle，credential_expiry，subscription_lifecycle，7 天常量 | 恢复窗口未可配置；远端断连接依实际能力、过窗迟到钱追踪缺；P2/P3 |
| D16 自动续费 | 未实现 | 无订阅级余额自动续费字段/任务（证书 AutoRenew 非订阅） | 默认关、稳定排序、余额竞争/互斥/提醒；P2/P5 |
| D17 累计计量/延迟超用 | 实现未验证 | metering/charge/flow_cursor，meteringstore/flow_batch/completion；zero_event_batch | 对账及超用边界实测；无零超用承诺、无节点预算协议；P1/P4 |
| D18 数据职责/额度快读 | 部分实现 | usage_quota、TrafficRecord、FlowUsage、NodeConfigPublish、observations、hourly | 明细兼作去重、历史重置与投影来源；职责未解耦；P4 |
| D19 可靠 ACK/分阶段 | 实现未验证 | zeroevent 文件持久缓冲、checkpoint；handler/zero_event_*、coverage | 在途/检查点失败及重启证据；接收/结算/统计状态展示需核对；P1/P4 |
| D19 观察与辅助失败 | 部分实现 | coverage 非阻断，principal observation，quota 独立读取 | RecordUsageProjections 仍在计量事务内，图表投影写错可阻结算；P4 需持久重建意图，不能直接吞错 |
| D20 归档/舍入/独立对账 | 部分实现 | batch 检查点逐样本舍入、reconciliation；流量去重仍依赖 TrafficRecord | 不可直接清历史流量；先独立去重/结算/汇总和有界回放，再保留；P4 |
| D21 单体/公共 jobs | 实现未验证 | application/background_jobs、capabilities/jobs、jobstore、Lease/Attempt | 独立 worker/同机伸缩/文件所有权隔离未证明；P6 基准前置 |
| D21 控制伸缩 | 未实现 | 当前有执行组/预算和状态读取，非伸缩器 | DB余量/CPU/RSS/插件成本/冷却/停领/在途；P6 |
| D22 双数据库 | 实现未验证 | datastore；migrations 0001–0036 + SQLite + schema reconciliation；CI mysql:8.4 | 本轮新建隔离 MySQL 容器、随机库；P0/P1/P2 各专项实库，P7 全升级故障矩阵 |
| D23 再认证/模块状态 | 部分实现 | frontend/utils/authSession、api/client、useRemoteResource、authLifecycle 测试 | 身份切换草稿/权限/不重放财务与全模块行为继续核对；P5 |
| D23 授权/负载/列表 | 实现未验证 | delivery projection、node group entity selector、quota/overview 分离 | 本轮原 49b622e 回归；P3/P5 |
| D24 基础工单 | 部分实现 | experience tickets + files/attachments，前端工单入口 | 订单/订阅/金额账务关联尚缺，机器人留插件；P5 |
| D25–26 验证/交付 | 部分实现 | .github/workflows/ci.yml、tools/acceptance、现有 benchmark | 本轮记录如下，未测容量不填数字；P0–P7 全部验收不得跳前置 |

## 权威状态、事务与副作用

| 领域 | 权威与唯一写入者 | 同库原子边界 | 外部/恢复 | 读模型 |
|---|---|---|---|---|
| identity | Users、UserAPIToken；identitystore | 当前身份/权限、token/审计 | 会话撤销、插件 generation 重验 | 脱敏账户/凭据目录 |
| commerce | Order、PaymentEvent；commercestore | 事件/订单/entitlement/凭据/quota event/审计/outbox | 插件核验渠道事实；重复 provider_event_id 幂等，未知远端不回滚已付业务 | order_queries 脱敏分页，不返回回调 |
| entitlements | Subscription、ProtocolCredential、QuotaEvent、mutation receipt | 订阅锁、额度/期/凭据、撤权意图、幂等审计 | jobs 后台发布；重置/过期失败整笔重试 | owned/admin subscription 与单订阅地址 |
| resources | Node、ProtocolEndpoint、Group、Deployment、NodeConfigPublish | 目标/确认 hash、generation/lease fence | Zero/SSH 外部动作，未知 reconciling；安装/升级队列边界保留 | readiness/授权可见性与节点观察分开 |
| metering | TrafficRecord、FlowUsage 累计游标；meteringstore | 去重/游标/计费/扣额/撤权/outbox（当前还耦合图表投影） | spool fsync 后 ACK，消费后 checkpoint，失败重放 | hourly/endpoint/current/coverage；观察不改业务授权 |
| jobs/extensions | run/attempt/lease、plugin installation/generation | 原子领取/fencing、重试记录/周期槽 | 插件 RPC、慢外部动作；取消不等于撤销副作用 | 持久历史和执行预算 |
| experience | tickets/replies/files；experiencestore | 权限/回复/附件关联 | 外部客服插件未接入 | 用户/管理员各自工单 |
| wallet（待建） | 不把 QuotaEvent 流量余额当货币余额 | 预留/扣款/退款/权益/撤权意图应单事务 | 外部出款另记，不自动执行 | 余额/来源/审计，P2 |

## 实施顺序和退出条件

| 阶段 | 小批次顺序 | 验收门槛 |
|---|---|---|
| P0 | 本决策、逐领域差距、事务、基线和迁移/测量计划 | 每个 D01–26 有定位/缺口；历史不冒充证据，无业务破坏变更 |
| P1 | 关闭自签支付事实越权；支付/权益/outbox/计量正常与故障回放纵链；保留原生发布核验 | SQLite/MySQL 原子回滚/重放，未知不重复业务和下发；未完成 P2 交易规则明确保留为缺口 |
| P2 | 不变购买快照/续费锁价→真实周期价值→货币账本/预留/混合→换购/退订/退款→恢复/自动续费→优惠宿主 | 三残值算例与财务并发、旧数据可信边界、渠道钱的迟到归属都有证据 |
| P3 | 端点/组/存量同步、保存交付、探测/运维边界与管理解释 | 迟报不隐藏、未交付不假可用、丢确认/旧回包/并发安装不误确认；失联期限不伪实现 |
| P4 | 独立结算/去重状态→可靠投影意图→有界读模型/诊断保留 | 归档后重放、舍入一致、投影故障不阻结算；未知覆盖不填零 |
| P5 | 会话/身份变更、业务关键失效刷新、交易与自动续费入口/工单关联 | 页面/API/持久化/审计共同交付；不自动重放写操作 |
| P6 | 可复现单进程基准→同语义独立 worker→受控同机伸缩 | 无重复财务/交付、队列损坏；慢 DB 不扩容风暴；量化收益/成本 |
| P7 | 基线升级/回滚/对账矩阵、双库及端到端/容量报告 | 每项完成有代码/测试/运行证据；剩余外部依赖分列，仅发布准备 |

## 兼容、迁移、回滚

不改已发布 0001–0036。新增迁移前 fetch develop 与核对编号；SQLite 模型迁移、MySQL 正式 DDL、已记账缺表恢复路径一并补齐。新快照版本可空/未知，不回填当前价冒充历史；旧已创建订单保留原商业承诺，任何新规则切换都携带明确版本。先增字段/表及只读核对，再切唯一写入者；不让旧新各扣一次。

故障矩阵至少：新库、从 7c7283f 升级、重复启动、迁移中断、已记录缺表/缺列、历史数据/待支付/待发布/未决保留。每步保留测试随机库便于失败定位，成功清测试库；不操作生产。回滚先停领新任务并核对租约/未知远端结果，备份 DB 与本地 spool/checkpoint；数据库反向变更不得删除资金/去重/结算证据。未落地金额/权益规则不能仅回滚二进制然后放旧 worker 重放。当前 P0 无 schema 变更，文档回退不影响数据库。

## 测试基线与测量方法

用户交接 CI https://github.com/zerodenet/zboard/actions/runs/39251135869 是历史通过证据，本轮需单独运行。云端 SDK 安装到 `/workspace/toolchains`，缓存写工作区，避免依赖用户 Mac 或修改仓库工具链要求。隔离 MySQL 8.4 绑定 127.0.0.1:33060，测试自行随机库；无实库成功不得标通过。

后端 `GOMAXPROCS=2 go test -p 2 ./...`、`go vet -p 2 ./...`、`go build ./cmd/zboard`、`CGO_ENABLED=0 go test -p 2 ./internal/datastore -run SQLite -count=1`。MySQL 设置测试 DSN 后 `go test -p 2 ./internal/datastore ./internal/handler -run MySQL -count=1 -timeout 15m`。SDK 按 CI tidy/test/vet。前端 frozen install/copy、当前 CI 六项排除的完整 test、build。按修改范围执行 architecture/OpenAPI/Compose/zrs-compiler；不增加排除。

性能先运行 `BenchmarkTrafficReportingAggregation`、`BenchmarkZeroEventReduction` 和现有 acceptance 工具，记录硬件/版本/规模/资源预算/命令/样本数。增加应用层并发业务读取+计量+插件负载，比较同环境基线及变更。报告事件率、活跃 flow/主体、批 SQL/写入量、DB延迟、核心 HTTP p95、积压、接收到结算延迟、CPU/RSS/DB索引/WAL/spool增长；耗尽至确认停服延迟与可观测超用单独计。没有真实节点就只能报告模拟限额/延迟，不能写真实停服或生产吞吐。分列总量/时间分布/覆盖缺口。

必须故障回归：支付重复/乱序/迟到/取消成功，手动/自动续费与退订/换购/余额调整竞争，插件超时/停用/撤权/负载，断网/确认丢失/旧回包/重启，DB慢写/回滚/积压/checkpoint提交失败，事件重复/代次/迟报/覆盖缺口，worker崩溃/重复领/扩缩/重启，归档迟到重放。按阶段拆测试，整体 P7 未通过前不宣称完成。

## 批次记录

### P0 — 2026-10-19

实际交付：本文件与决策文件；代码审计发现自签支付事实权限缺口、锁价/换购与余额缺失、计量投影/明细耦合。无业务行为或数据库变更。当前固定 Go/Node/pnpm 已准备，MySQL 8.4 隔离容器可 ping；Go 首轮检查因默认缓存只读及部分模块代理 403 未完成，正在在工作区缓存恢复依赖。前端固定安装成功。未将环境阻断标为测试失败或业务通过。提交、PR、最终命令结果随下一批追加。

### P1-1 — 支付信任与跨库幂等恢复（2026-10-19）

实际代码：`identity.IntegrationScopeRequiresAdministrator`、identitystore 签发/Resolve/原子 Admit 及 commerce 最终事务重验，关闭普通用户自签支付事实入口；已有普通用户旧 scope 也拒绝执行，只读 scope 不变。管理员集成仍只处理本人订单；已准入 native 支付插件继续通过订单绑定宿主授权服务普通用户。新增行为测试同时跑普通/管理员 native 插件、旧角色/旧授权、真实 HTTP 签发与调用，以及 SQLite/MySQL 事件→订单→权益→凭据→quota event→outbox 原子回滚/恢复重放。

真实 MySQL 新回归发现 JSON 列规范化使原始字符串比较拒绝相同事实重放，已改为对不可变字段逐项比较（时间按同一瞬间），变金额/币种/交易号/时间仍冲突。回调改为先锁订单再核验账户，与管理员结算/取消顺序一致；实库八路混合管理员确认与支付事实竞争验证只履约一次、只发布一代。业务凭据状态不代替远端确认，支付不会生成已成功部署记录。

数据迁移：无 schema 变更、无数据删除。兼容差异是普通用户既有支付写 scope 即时失效，需使用已准入 native 支付插件；管理员权限撤销也立即生效。不能以回滚二进制恢复有越权风险的自签入口。MySQL JSON 已有事实继续按内容幂等，不重写历史。

验证（Go 1.26.8 / Node 22.23.0 / pnpm 11.9.0，云端，无生产操作）：

- `GOMAXPROCS=2 go test -p 2 ./...` 通过；最终日志 `/tmp/zboard-p1-backend-verified.log`，handler 113.261s。环境驱动的真实 Zero 节点验收未配置，不算通过。
- `ZBOARD_TEST_MYSQL_DSN=<隔离127.0.0.1:33060> GOMAXPROCS=2 go test -p 2 ./internal/datastore ./internal/handler -run MySQL -count=1 -timeout 15m` 通过；`/tmp/zboard-p1-mysql-final.log`，datastore 37.388s、handler 113.261s。包含近期缺表已记账发布字段恢复回归；不是 SQLite 或 DryRun 替代。
- `go vet -p 2 ./...`、`go build -p 2 -o /tmp/zboard-p1 ./cmd/zboard`、`CGO_ENABLED=0 go test -p 2 ./internal/datastore -run SQLite -count=1` 通过。
- 前端 frozen/copy 安装、当前 CI 原六项排除 test：871 通过、8 个原有 skip；build 通过。无新增排除。`/tmp/zboard-frontend-p1.log`、`/tmp/zboard-frontend-p1-build.log`。
- Redocly 1.34.3 OpenAPI 通过，282 项既有警告；`git diff --check` 通过。SDK 未改动，独立 test/vet/tidy 另行记录。
- 依赖代理对部分 Go zip 返回 403；从其真实 Git/GitLab 上游同 tag 获取到工作区缓存，现代 C 模块仅重写 module zip 根路径后由 Go 按原 go.sum 校验成功；仓库 go.mod/go.sum 未改。最初错误测试把业务凭据 active 当交付确认，已修正为核对真实 deployment，未改业务状态语义。

提交对应 `fix: secure payment fact authority and replay across databases`；Draft PR https://github.com/zerodenet/zboard/pull/78。本批关闭 P1 的两个真实缺口，不代表 P2 金融规则或全部真实节点端到端已完成。下一批 P2 优先基础锁价。非整除周期实付余分与跨付费周期锁价两项已提出具体案例，等待业务答复；其他工作继续。余额/优惠协议/失联期限内核能力与 P2–P7 其余差距仍按上表执行。
