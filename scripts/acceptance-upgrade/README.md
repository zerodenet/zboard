# 隔离的旧版本升级演练

此工具从 Git 对象导出精确 `7c7283fb34840883fccaba1de6e0a98edb24dfb1` 与指定新提交，分别编译两个真实版本的测试探针。旧源码实际生成旧库，当前源码迁移独立 SQLite 副本或旧探针自己创建的随机 MySQL 库。需要干净且提交精确匹配的 checkout；输出、备份和失败日志保存在被忽略的验收目录，不重置工作区，不自动重试。

先只读检查参数与模板哈希，再显式执行：

```sh
python3 scripts/acceptance-upgrade/run.py --checkout "$PWD" --revision "$(git rev-parse HEAD)" --driver sqlite
python3 scripts/acceptance-upgrade/run.py --checkout "$PWD" --revision "$(git rev-parse HEAD)" --driver sqlite --execute
```

MySQL 需要明确选择已经存在的隔离测试容器，并通过环境配置测试 DSN。探针在创建随机数据库前核对该容器 3306 的 loopback 映射与 DSN 相同，仅接受 `127.0.0.1` TCP。原 DSN 的数据库名不作为迁移目标；由旧版本 fixture 创建并正常清理自己的随机 schema。

```sh
python3 scripts/acceptance-upgrade/run.py --checkout "$PWD" --revision "$(git rev-parse HEAD)" --driver mysql --mysql-container YOUR_TEST_CONTAINER --execute
```

也可选择 `--driver both`，按 SQLite、MySQL 顺序执行。只编译一份旧/新探针对，两个数据库各自使用独立夹具、备份和逐阶段结果，减少重复编译及保留二进制的空间成本；不会把一个数据库的通过当另一个数据库的证据。第一组失败立即停止，后续组不自动启动。该模式仍要求显式隔离 MySQL DSN 和容器。

工具不创建容器、不改变服务器安全设置或授权、不连接外部支付及内核。测试账户需要拥有该隔离实例中的创建/删除随机数据库能力；不会自动配置账户。MySQL 旧探针关闭自己的池后，用容器内 `mysqldump` 生成保留的逻辑备份，再等待控制器完成升级核对。新增恢复探针使用旧基线二进制，把 SQL 备份导入该隔离实例中另一个新建的随机 schema；SQLite 则恢复到另一个独立文件。两者均核对六张权威表的全部原列；MySQL 只在确认创建、导入及核对通过后正常清理自己创建的恢复 schema，失败保留确切目标及观测证据。没有把当前已产生新业务写入的数据库降级或覆盖，也没有验证第二台数据库服务器恢复。凭据仅通过环境传递，不写入命令参数或公开回执。全部夹具金额和节点凭据都是隔离测试数据。

检查原订单、订阅、协议凭据、流量账、额度事件和未决发布的所有旧列；重复迁移不改变它们。通过当前共享应用入口补登记旧库唯一报告的身份，不补造历史付款或诊断来源。随后检查旧已付订单/计量重放、未知历史价值、管理员明确零元整份终止、另一订阅保留及持久撤权意图。再次启动新进程，核对八张相关表与相同回执，旧已付订单不能复活已终止订阅。

后台执行器和远端节点均未启动，发布队列中的未知结果是明确的故障夹具。持久撤权意图不代表远端已经撤权。这个演练不等于完整 P7、浏览器/真实支付/内核集成、原二进制回退或生产验收。已执行的精确版本、探针 SHA、早期控制器错误及未执行项见 [2026-10-26 记录](../../backend/architecture/validation/2026-10-26-baseline-upgrade.json)。

新增备份恢复及旧报告身份路径必须以本次模板的实际执行结果为准；旧的 690e55c 私有探针成功不能作为此入口或新模板已经通过的证据。控制器不增加原 5 分钟单探针门槛，失败不自动重试。

本次公开入口已实际执行：精确 `edfd36e7a5159a37fa2694628507366c20eb93a6` 使用 `--driver both --execute`，两库升级、进程重启核对及旧备份独立恢复全部通过。原备份摘要保持不变；探针、二进制、逐命令终态与未执行范围见 [公开入口验证记录](../../backend/architecture/validation/2026-10-26-public-baseline-upgrade.json)。该提交自己的 CI [40325636377](https://github.com/zerodenet/zboard/actions/runs/40325636377) 六项成功。

## 0064–0069 的新权威数据恢复探针（新增，待执行）

控制器现在为新版本注入 `current_authority_probe.go.txt`。原来的旧库生成、六张旧表原列核对、旧支付/计量重放、明确零元终止及新进程恢复检查保持原顺序。其后，使用同一已升级的隔离数据库，经真实核心购买、余额支付、原生计量、证明封存、订单取消与自动续费暂存路径，生成：

- 0064：完整可解压核对的原生证明及原始 nonce/epoch/金额绑定；
- 0065：丢失 Reserve ACK 的 unknown、待 Commit、待 Release 三类持久意图；
- 0066：真实未扣款的 automatic pending order/consent/end/epoch/attempt；
- 0067：观察材料预算与明确的 compact retirement/gap。大正文是内部应用/适配测试输入，不冒充 HTTP 请求已被接纳；原 HTTP 上限仍为 1 MiB。

新增探针保存新版本全库快照，再以新版本二进制恢复到另一个新文件或新随机 schema。它比较选定的38张权威表全部列（包含操作人、原因及金融/材料处置审计的 audit_logs），二进制 payload/parts 以 hex 表示，不把 gzip 转为有损 UTF-8。恢复后重复迁移、完整证明读取、原生有限重放与检查点失败重试、退役 gap 重放及预算与实际 parts 字节核对均需通过；原已确认的 automatic pending 只能扣款一次，unknown ACK 先 Query，原 Commit/Release 意图按同一 key 恢复。提供方仅为人工序列化进证据文件的进程内 map 夹具，用于验证 Host outbox 恢复分支；它没有证明插件自身具备持久存储、崩溃恢复、真实库存能力或真实外部沙箱。

**新版本备份用新版本恢复；旧版本备份仍单独用旧版本恢复。** 不把有新业务写入的数据库直接交给旧二进制降级。原数据库及两份备份均不被恢复目标覆盖，备份哈希前后保持一致。恢复失败时保留新目标与证据；成功后仅正常清理本次新建的 MySQL 恢复 schema。SQLite 新文件、备份、导出源码、二进制及日志全部保留。

新增 `--artifact-root` 只决定本轮新随机子目录的位置，不移动任何现有数据库、缓存、旧备份或日志。默认仍是仓库忽略的证据根；同一云端环境可明确选择 `/tmp` 空闲文件系统用于本轮新副本。P6 原容量测试的数据盘要求与门槛完全不变。

控制器记录真实 Git backend blob 大小，另以每个测试二进制256 MiB、每个数据库的备份/副本128 MiB、编译临时512 MiB、增量缓存256 MiB、日志32 MiB及每文件系统256 MiB余量做事前估计；相同设备合并预算。MySQL 还核对显式测试容器 `/var/lib/mysql` 的可用量，并在 checkout 设备保守预留其可能共盘的128 MiB增长。预算不足时在导出、编译和数据库创建前返回 `not_started`。这些是小夹具的工程估计，不是硬写入配额、冷缓存编译峰值承诺或吞吐验收。预算参数必须记录，不能用其代替 P6 原门槛。控制器另外记录实际二进制大小、新产物逻辑/分配字节及两盘最终观测；无自动清理、付费扩容或安全配置变更。

在0066–0069真实代码、迁移和夹具都已提交、工作区干净且其他 workload 结束后，先 preflight，再由同一精确头单次执行。例如：

```sh
GOCACHE=/workspace/toolchains/cache/go-build python3 scripts/acceptance-upgrade/run.py --checkout "$PWD" --revision "$(git rev-parse HEAD)" --driver both --go-bin /workspace/toolchains/go/bin/go --mysql-container YOUR_EXISTING_TEST_CONTAINER --artifact-root /tmp/zboard-p7-artifacts
# 检查完整预算和精确源码绑定后，在原参数上仅加 --execute。
```

新增探针当前仅准备、格式与语法检查；必须以其实际命令终态和新精确头记录更新此段。2026-10-26的 `edfd36e7` 成功只覆盖当时模板，不能覆盖本增量。保留每个原5分钟探针门槛和所有历史失败，不自动重复执行或把阶段通过写成整体P7/生产验收完成。

0067负责代理已只读确认版本名、两张表及主键稳定：预算表固定id=1（retained_bytes/revision），退休表intent_id，包含header_sha256/state/cause/source_summary/next_part/verified_bytes/hash_state二进制/delete_part/remaining_bytes/content_verified/review_at/created_at/updated_at。探针动态冻结这些全部原列，不复制其业务实现。超过2MiB的内部source会自然产生complete/source_size_limit退休回执、无parts且不增加预算；同一输入晚到重放不得重建正文或改写权威状态。仍须以0067最终冻结源码和实际执行结果为准。

后续0067增加第三张 node_observation_retirement_parts，主键(intent_id,ordinal)，保存每段 SHA；本探针把该表全部列与真实二进制 hash_state/parts 一起核对。除大小准入complete场景外，探针还对真实未被引用的pending小source注入8天后的时钟，调用原Retire(ctx,1,8MiB)到deleting检查点，要求非空逐段SHA，随后将其hash_state/NextPart/DeletePart/RemainingBytes随全库备份并逐列恢复。仅新恢复目标继续原状态机到complete，并核对账务、凭据、累计游标和已有审计前缀不变，再重放计量及gap；这段新增代码仍未编译/运行，不能称为已验证。新版本 MySQL 全库备份显式使用 --hex-blob，避免 gzip、hash_state、parts 等BLOB通过连接字符集转义造成歧义。

0067负责代理最后确认私有候选已冻结，integration.patch SHA256为980034702c42c93fb7826966ad497695a6f4faeee0355dc7a345fc7f16df24a0；本探针仍要绑定最终提交源码并实际执行，不以该候选SHA替代验收。


0068/0069补充（本轮私有代码，不是既有验收结果）：选定矩阵扩大为38张表，额外逐列核对subscription_flow_start_events、fair_use_node_coverage、principal_flow_node_generations、principal_flow_observations、principal_flow_currents、principal_flow_scope_observations及实际数值状态principal_flow_scope_currents。探针在同一旧库升级后的新版本DB，用真实capability生成超过128字符且共享长前缀的两个独立运行代次，保留完整原文及0069索引digest，再写迟到的旧代次观察；当前活动必须维持第二代的3，不能被旧代99覆盖。重启和新备份恢复要求每个原字段、generation.start、current、scope及历史digest都保持；恢复后显式重放原boundary/observation/flow-start，不能重置窗口、创造历史或改动账务。

这些是隔离应用/适配路径的新代码验证，不冒充真实远端内核已部署。HTTP原nodeReportMaxBodyBytes仍为1MiB；MEDIUMTEXT按单identity2MiB的辅助存储/读取预算及总量预算处理（该保守内部预算不扩大HTTP接收），完整原文不是无限输入许可。该升级仅修复合法长ID被旧128列拒绝及读取放大，不声称所有观察历史表的总体积恒定；flow-start既有15天保留与principal历史/必要ledger线性增长应分别报告，64MiB optional parts上限不是全库上限。本候选不删历史/数据库/旧备份、不改任何原计费舍入、原迁移或容量门槛。0068/69新精确源码仍需实际编译、双库执行及CI结果；所有旧阶段通过不可挪用。
