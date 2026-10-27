# P6：本机投影进程控制组件

本批交付真实进程控制、资源采样、调度数据库压力观察和策略组件。默认策略MaxWorkers=0，管理员对部署收益的确认默认false；确认只是部署前提声明，不是程序自动验证收益。主服务/application/CLI接线将随后续可编译集成批次交付，本批不启动新的默认后台进程。

jobs Runtime用固定64样本记录真实Claim/Finish完成延迟和错误，空队列读取不能充当成功数据库写入。策略结合持续队列积压、数据库写入/连接余量、主机和cgroup总CPU/内存、IOwait、实际存储余量，保留资源和冷却。插件与子进程计入主机/cgroup观测；未知、过期、慢写、错误或资源压力不授权扩容。全局Claim、任务身份、预算、资源fence与幂等不变。

Linux控制器实际启动同程序子进程，记录Wait终态，发送SIGTERM走既有排空流程，不在旧进程尚未退出时补开副本；异常退出使用有界退避。所有者文件使用非阻塞flock、拒绝符号链接/非本人/非私有文件；不截断、删除或抢锁。子进程继承锁描述符，父崩溃后未退出的旧worker继续保护所有权。worker采用nice19、单Go CPU及Go内存目标；GOMEMLIMIT是GC目标，不是硬RSS隔离承诺，资源保障和收益仍须容量实测。

本地实际验证：`GOMAXPROCS=2 go test -json -p 1 ./internal/localworkers ./internal/capabilities/jobs -run 'LocalWorker|RuntimeDatabasePressure' -count=1 -timeout 15m`，30个测试事件通过，无失败/跳过，wall1.784秒。源码范围为上述两包和module锁，哈希前后一致；其他并行领域没有计入本批验收。真实主程序、两库竞争、慢写/插件负载端到端及收益对比尚未完成，不能将组件测试当作P6整体完成。本批无数据迁移或外部节点操作。
