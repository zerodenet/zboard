# 默认投影任务共享组合

P6 前置子批。application 的 UsageProjectionJob/NodeObservationJob 返回现有两类投影的 PeriodicJob；默认 handler 入口实际使用这些定义。定义本身不注册、不打开领取门禁，也不访问数据库。RegisterPeriodicJob 仍为唯一持久身份和调度策略组合入口。

## 保持的执行合同

- usage_projection：每秒、30分钟超时、每批200。
- node_observation：每秒、30分钟超时、每批32、8MiB。
- 既有 immediate、reconcile-after-loss、retry、system owner、core:<id> resource、revision1、3次尝试/1秒退避与零值规划策略保持。
- transport 的重复启动防护和 observer 包裹复用原实现；仅注册成功后记录本地 loop。全局管理员停用、进程 Drain/Detach/Close、foreign lease 和恢复边界不变。
- capability 在调用时读取，Ready/Run 原错误和结果向唯一既有重试规则传递；满批返回 nil 不自动改成 continuation。

通用周期任务保留原 literal 策略与就绪规则，其投影分支委托同一 application Ready。无新SQL、模型、迁移、账务身份、配额扣减或舍入变更。

## 验证范围

新增测试经过真实默认入口、Runtime、jobstore 和 Drain，核对两类 durable schedule/attempt、observer、重复启动、成功/重试、批量/字节边界、关闭后注册失败；领域仓储用 probe 隔离组合，不能冒充领域 SQL 或真实远端集成。另验未准备 SQLite 上 New/定义/Close 无SQL、无建表或注册。

既有周期 outcome、全局取消与本地关闭、领域投影和完整后端回归必须实际执行。原170个handler MySQL测试函数及154份发布SQL静态保持不变；新MySQL wrapper 显式复用既有业务fixture。行为等价重构不要求旧实现故意失败。

冻结7文件源码实际验证：SQLite专项52.008s、真实MySQL专项36.857s；SQLite及既有生命周期race/count3为345.223s，MySQL race/count3为107.316s，均通过。完整后端344.307s、vet11.729s、build1.894s、CGO=0 SQLite38.355s通过。无DSN组保留既有MySQL opt-in skip；实际MySQL两组无skip。完整后端缓存输出和包/用例/跳过列表另列，不把中途进度当完成。

[冻结源码及全部本地结果](validation/2026-10-22-shared-projection-composition.json)。首次静态证据生成器的receiver解析错误仅为工具错误，原记录保留；没有测试失败被改写。提交后的精确CI和完整MySQL另在PR及接续记录报告，不借376六绿替代。

## 迁移和回退

无schema或数据迁移。回退组合代码无需数据库回滚，既有 durable ID/resource/revision 与政策不变；保留队列、attempt、lease、财务和计量来源。无生产操作。

## 尚未交付

无独立 worker 命令/进程、额外并发或伸缩器。未来投影 worker 仍须先用只读前置检查，不能 PrepareSchema/补默认/bootstrap/打开HTTP、spool或插件；只组合被选定任务，不能借 handler.backgroundJobs 注册其他执行器。维护门禁仍有既有1秒缓存，Drain超时后Close仍等待本地完成，均不承诺新硬期限。

既有 core:<id> 和全局预算互斥不能证明同队列多进程吞吐收益。必须先测得隔离收益，再扩展 worker；插件忙负载/慢DB/持续增长/平滑伸缩仍待验，P6/P7未完成。P4 source/parts重放与安全保留仍独立未解决。


a092精确提交已有自己的六项CI和本地完整MySQL三命令全通过，见计划末节及
final-ci/final-mysql收据。接续发现的既有周期panic观察误报另见
[专门修复](core-convergence-periodic-panic-observation.md)，该新头仍需自己的验收。
