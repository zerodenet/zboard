# 投影数据库只读前置检查

本批为P6前置子批，不提供worker、跨进程吞吐或自动伸缩。默认单进程启动保持原逻辑。

## 实际入口与唯一维护规则

```
zboard -f /path/to/existing-config.yaml -check-projection-database
```

与migrate-only、prepare-hourly-rollback互斥。检查入口在普通凭据构建/迁移、
PrepareDatabaseSchema、默认配置、bootstrap、Services、HTTP路由和spool之前退出。
成功只表示检查时投影依赖可验证，输出明确的“no worker started”。错误按阶段分类，
不打印DSN、账号授权、源配置值；保留errors.Is/As供内部核对。

校验范围是usage与node-observation实际依赖：任务预算/组/dispatch/运行/attempt/
schedule，pending/immutable traffic source/hourly/daily，nodes资产/计数、0060头/
parts/state，以及维护配置/迁移任务。只要求这12个拥有依赖的receipt及实际物理结构；
不把无关商业receipt缺失或合法保留的旧开发history误判为依赖不足，也不认证完整
商业库或未来版本兼容。已发布的154份SQL和模型均不修改。

必要列类型/unsigned/NULL/主键、完整顺序索引、来源保护FK、0054切换/旧触发器、
0059/0060现有结构合同、正全局预算和实际SELECT可见性分别检查。旧SQLite小时键
为0024发布的TEXT，不当作datetime错误拒绝。维护源复用b58唯一严格读取路径；
缺失/非法值不补默认，缺task_id仍走既有active task/reservation回退。

## 连接与权限边界

SQLite单独以mode=ro/query_only/deferred打开既有文件，不注入普通打开器的
journal_mode/synchronous配置，不创建缺失文件。memory、非本地URI、显式_pragma、
immutable/nolock无法在该入口证明可靠只读实时观察，返回unsupported。

MySQL保持原配置DSN/池参数，在READ ONLY事务中验证。触发器元数据零行不等于
没有触发器；必须消费完整SHOW GRANTS，证明当前schema/global直接TRIGGER或ALL，
并保守拒绝REVOKE、角色或不明限制，正授权出现顺序不影响结果。不创建账号、
改密码、GRANT/REVOKE或修改服务器设置。独立SELECT-only账号的实际权限未验；
纯parser/driver反例不冒充实库权限测试。

## 验证与失败记录

每次冻结源码及日志按哈希保存。最初静态稿编译失败（缺collation字段），之后实际
SQLite发现错误拒绝已发布TEXT小时键，命令测试无效配置导致失败；重命名测试表
在失败后影响后续负例的问题改为每例cleanup隔离。旧失败不改为通过，不改原断言/
阈值或排除。最终冻结执行结果由单独validation收据给出，未执行项不得推断通过。

物理schema和实际业务行逐表编码比较前后；拒绝尝试的no-op DML/DDL；实际main
子进程覆盖正常检查、模式互斥、坏schema拒绝且不修复、缺文件不创建，以及不启动
bootstrap/凭据迁移/本地文件存储。真实SQLite物理只读池、MySQL READ ONLY、
metadata Query/Scan/Rows.Err/Close故障、旧history及缺失/错序/错误类型等负例另验。

## 兼容、回退与仍缺

无新迁移、无数据/授权/金额写入。回退代码不需数据库回滚；旧二进制不认识新flag，
运维调用应同步回退。检查是时间点证明，不能与并发DDL构成锁；部署准备必须在
worker启动前完成，后续真实worker每次领取仍需维护门禁，不能凭本次成功永久放行。
现有hourly恢复检查可能读取事实范围，不承诺常数时间启动。

仍缺投影执行器共享组合/独立进程、实际隔离收益、资源预算/冷却/平滑扩缩和容量
竞争验收。P4 source/parts安全保留、held恢复与总增长也未完成。本命令不冒充这些交付。
