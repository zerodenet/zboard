# 投影只读预检的0061依赖

P4/P6前置一致性修复，基于8ae692e。当前node-observation worker的ACK会读取material_retired_at并写material_review_at，0061之后旧的12回执预检仍可能放行只有0060结构的数据库，随后worker无法提交ACK。

## 实际变化

只读预检要求第13个实际依赖回执0061，并复用inspectObservationMaterialSchema核对两列存在、类型/空值和原默认值契约。缺回执、已记录丢列或错误结构返回既有typed ProjectionValidationError；没有调用reconcile、自动建列/索引或补回执。

0060基础observationColumns保持原阶段职责，避免破坏0060→0061的分期准备。预检仍只认证usage和node-observation实际依赖，不将未选择的材料回收扫描索引或无关commerce回执扩成必需。既有全表数据/DDL快照、只读SQL门禁、维护权威、每次领取暂停及并发DDL须停worker的边界保持。

没有新增schema/SQL或生产迁移，157份正式SQL保持。正常程序准备数据库后仍可通过。回退该检查会恢复旧的错误放行，不应删除0061证据或用旧预检结果启动新worker。

## 验证

旧检查实际错误放行缺0061回执和缺review列。首次第三个故障场景受前一断言提前退出、夹具未恢复的缺索引连带影响，该部分不计作目标复现；81.300s原失败保留。修正后每个故障由Cleanup独立恢复，原只读/typed-error断言保留。

首轮修复37.632s中，三项恢复后的正向检查放在Cleanup内，Go已先取消t.Context，导致table metadata错误；已将正向检查放回有效测试主体，Cleanup只恢复夹具，原失败保留。最终7个Go顶层零skip，116.117s；实际MySQL8.4的共享只读矩阵1顶层零skip，21.437s，缺回执/两列拒绝、正常恢复及原完整矩阵通过。

Go范围包含实际SQLite main/flag/config/open/只读验证子进程，核对不启动HTTP/spool、不补bootstrap或迁移凭据，以及坏schema/缺文件/互斥模式拒绝。MySQL在此为应用/真实只读事务路径，未冒充独立MySQL CLI或OS故障验收。两个无匹配MySQL包不计覆盖。

[全部命令、来源、用例、日志哈希和原失败](validation/2026-10-23-prepared-observation-material.json)。最终Go/MySQL源码哈希相同；无前端/SDK改动。新提交CI仍待推送后核验；8ae结果不认证本修改。本头完整本地backend4/MySQL3/race3受原环境存储限制未执行，不降低门槛或扩大清理。

这只修复准备检查，尚无独立worker/自动伸缩或收益证明。P4总增长/有限重放、P6容量/伸缩及P7综合、外部/生产验收继续未完成。
