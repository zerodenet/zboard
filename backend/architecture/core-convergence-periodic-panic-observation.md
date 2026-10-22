# 周期处理器异常退出的运行观察

## 实际问题与行为

通用周期处理器及usage/node-observation默认入口原来在defer中使用具名返回err完成进程内观察。Run发生panic时err仍为nil，导致Registry显示succeeded/Failures0，而同一次真实Executor完成事务已将run/attempt/schedule记为unknown。两种状态不一致会误导运行诊断。

现在仅在既有观察shell记录Run是否正常返回。未返回时用固定安全错误完成观察；正常返回仍走原error/continuation分类。没有recover、读取或拼接panic值。原panic继续进入Executor唯一既有unknown完成路径，不增加Retry包装、重试次数或第二条任务/财务写入路径。

## 实际验证

相同测试源码在旧a092生产实现上运行，SQLite三个入口均在durable unknown/单run/单attempt检查后，实际因观察器误报成功失败，raw Go exit1、18.144s。旧失败不改成通过。回归用例的领域仓储为panic probe，真实Runtime/jobstore/完成事务/Registry使用实际SQLite或MySQL。

修复冻结2文件后的SQLite专项19.699s、真实MySQL专项49.670s已通过。SQLite及既有生命周期race/count3为409.351s，真实MySQL race/count3为138.558s，均通过；完整后端346.315s、vet11.411s、build2.049s、CGO=0 SQLite38.792s通过。[冻结源码、旧失败及全部结果](validation/2026-10-22-periodic-panic-observation.json)。提交后的精确CI和完整MySQL另在PR/接续收据报告，不借a092六绿替代。旧continuation/unknown/foreign lease/Drain与默认投影组成回归一起执行，无DSN组会跳过MySQL opt-in（包括本批新增实库wrapper）；它在单独的实际MySQL专项及race组执行，不以无DSN跳过冒充通过。

## 兼容与回退

无SQL、schema、迁移、资金/权益、来源/去重/游标、调度身份/策略或授权变更。现有Registry失败字段即可展示，无API/前端变更。回退仅代码恢复，会重新引入异常退出误报成功；既有unknown任务、attempt和待核验依据必须保留，不能据此自动重试远端操作。

## 范围与未完成

仅修复仍在同一进程内被既有Executor接住的panic之观察。SIGKILL/进程崩溃、真实内核或支付插件不由这些probe测试验收，外部执行是否成功仍需原核验。无新worker/伸缩器或已证实容量收益。P4安全保留、持续增长、资源竞争与P6/P7其余验收保持未完成。
