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

工具不创建容器、不改变服务器安全设置或授权、不连接外部支付及内核。测试账户需要拥有该隔离实例中的创建/删除随机数据库能力；不会自动配置账户。MySQL 旧探针关闭自己的池后，用容器内 `mysqldump` 生成保留的逻辑备份，再等待控制器完成升级核对；SQL 备份没有在本工具内恢复到第二台服务器。凭据仅通过环境传递，不写入命令参数或公开回执。全部夹具金额和节点凭据都是隔离测试数据。

检查原订单、订阅、协议凭据、流量账、额度事件和未决发布的所有旧列；重复迁移不改变它们。随后检查旧已付订单/计量重放、未知历史价值、管理员明确零元整份终止、另一订阅保留及持久撤权意图。再次启动新进程，核对八张相关表与相同回执，旧已付订单不能复活已终止订阅。

后台执行器和远端节点均未启动，发布队列中的未知结果是明确的故障夹具。持久撤权意图不代表远端已经撤权。这个演练不等于完整 P7、浏览器/真实支付/内核集成、原二进制回退或生产验收。已执行的精确版本、探针 SHA、早期控制器错误及未执行项见 [2026-10-26 记录](../../backend/architecture/validation/2026-10-26-baseline-upgrade.json)。
