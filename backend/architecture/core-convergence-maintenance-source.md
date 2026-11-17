# 后台维护源：未知状态停领

## 实际问题与边界

`platformstore.maintenanceData` 原先忽略 `ParseBool/ParseUint` 错误；
缺少 `maintenance_enabled` 也被当作 false。应用的 `WorkPaused` 已有错误时
暂停规则，但错误未从持久源传播，损坏配置可能令后台继续领取工作。

本批仅改变该读取路径：缺失/空/NULL/非法 enabled、已存在但非法的 task_id
返回既有 `ErrMaintenanceIncomplete`，不带入原始值，不插入默认值、不写配置。
合法 ParseBool 形式、零前缀 task_id、旧 revision=0 保留。
旧库缺失 task_id 仍通过 active migration task/既有 reservation 检查；不把它
误当作配置损坏。查询错误原样传播；恢复可靠源并使缓存失效后可恢复工作。

既有外进程 running lease、queued intent、attempt、schedule 与审计不被读取或
暂停动作取消。默认 HTTP 启动原先已在 StartWork 前调用真实默认配置初始化；
本批不增加启动迁移、worker CLI、权限变更或生产操作。

## 测试基线与失败保留

最初三个文件候选的专项双库/race 通过，但完整 backend 暴露32个 handler
顶层（49个父/子失败事件）仅迁移而未准备合法维护源的正向 fixture。
原始失败保留于独立 freeze01；没有排除、放宽断言、延长超时或共享工厂补种。

新候选基于1012，19文件：相同生产解析/应用合同，14个既有 handler 文件的
32处明确正向准备及一个 test-only helper，CI首条 MySQL 命令追加 application。
helper仅选择既有 maintenance_enabled 定义，调用既有真实初始化器并失效缓存；
不填 task_id 或其他配置，不放入读断言/共享工厂/故意破坏后的步骤。
去除这些准备及局部变量绑定后14文件逐字恢复1012；70个 opt-in MySQL
业务函数、原有工厂、154份已发布 SQL 字节不变。

验收收据另行记录实际新候选执行；静态冻结不算通过。旧实现对相同缺失/非法
来源测试必须真实失败，新实现必须通过；SQLite/MySQL包含源中断/恢复、合法
旧格式、外 lease 保留、读取无写及 race。完整 backend 与精确新头 CI 独立执行。

## 兼容与回退

没有迁移或业务数据重写；正常安装已有初始化顺序保持。故意删坏维护源时应
先通过既有受控配置/恢复流程修复真实来源，不能由普通读取静默补默认。
本次代码可回退且数据库格式不变，但旧代码恢复了原有错误被忽略的行为；
不能把回退宣称为同等安全语义。此批不提供自动修复或新授权旁路。

P6只读 schema 前置验证、独立 worker、受控自动伸缩和长期资源保障仍未完成。
