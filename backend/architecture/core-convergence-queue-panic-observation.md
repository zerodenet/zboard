# 发布与计量队列异常退出观察

## 行为和业务边界

发布观察原来在PublicationQueue.Execute返回后才安装finish；计量观察在投影/Commit正常返回或返回error时手动finish。两处遇到panic都可能留下Running1/LastResult never；成功重放后仍有一个未结束观察。

发布观察现在在调用能力前建立退出保护；只有整个正常返回才按原failure完成观察。计量退出保护只覆盖已经开始的投影/结算/checkpoint段，原各条error/success完成点保留；正常完成后不再重复finish。两者都不recover、不读取panic值，只写固定安全说明，原异常继续传递。

不修改发布Complete/续租/租约回收/原生核验；panic仍保留原待核验发布及有效lease，不变成失败重试。计量已提交事实不回滚、不重复计量；checkpoint panic不推进位置，沿原去重与累计游标重放。没有新ACK、任务、业务授权或金额写入路径。

## 实测和历史失败

初始旧SQLite两个反例实际失败16.141s；随后固定生产源码SQLite通过22.181s，但新MySQL节点fixture遗漏Config，JSON列以3140拒绝，54.857s失败发生在进入处理器前。保留这些冻结和原日志，只给新测试节点配置合法{}；未改原factory、断言、超时、SQL类型或生产规则。

修正后的同一测试源码再在旧生产上执行：SQLite54.592s/MySQL11.359s均raw Go exit1，两个用例仅因未结束观察失败（三条观察断言/数据库）。发布lease前后完全相同；checkpoint故障后已结算30字节，位置0，重放后位置1/仍30字节的检查均实际执行。

最后固定源码freeze04与freeze02生产字节相同，测试与修正后的双库旧反例一致；SQLite专项23.339s/MySQL专项53.343s，race/count3为432.059s/153.194s，均通过；完整后端347.050s、vet11.006s、build1.886s、CGO=0 SQLite39.725s通过。[冻结、全部失败/控制/最终结果](validation/2026-10-22-queue-panic-observation.json)。无DSN全量会跳过MySQL opt-in（含本批两个新wrapper），它们在实际MySQL专项和race组执行、零skip。提交后的精确CI/完整MySQL另记，不借a7六绿替代。

## 兼容、回退和限制

无schema/迁移/模型/Store/计费舍入/内核配置或插件接口变化。回退只恢复观察处理，会重新引入未结束计数；保留现有发布租约、目标/确认、raw计量事实、游标和checkpoint，不删除状态或自动重新发布。

发布测试使用真实SQLite/MySQL队列但执行器是panic probe，并未连接真实内核。计量使用既有checkpointTestSpool注入Commit异常，实际SQL账实/游标重放；不冒充FileSpool fsync/ACK、进程重启或SIGKILL验收。空batch和ReadBatch前置失败不在已开始的消费观察范围；已完成消费后的辅助metrics不改变原消费完成点。

独立worker/伸缩、当前原阈值容量、长期增长与P4安全保留仍未完成；历史112次503及两次376容量预检退出继续保留。
