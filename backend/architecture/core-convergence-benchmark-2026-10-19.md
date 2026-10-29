# 计量微基准起点（2026-10-19）

这是 P0 测量计划的首份实测补充，不是 P6 容量验收，也不构成吞吐/延迟承诺。比较原基线 `7c7283fb34840883fccaba1de6e0a98edb24dfb1` 和 P2-5 `16815a9288db2bb852fdc0065a7551d726ab517e`；未修改计量结算实现来迎合结果。原始逐样本指标及二进制 SHA256 见 [JSON 证据](validation/2026-10-19-metering-microbench.json)。两端最终账务用量断言均通过。

## 环境与复现

云端 Linux x86_64，AMD EPYC 9V74；可见5个CPU，cgroup配额4核、内存上限16GiB。Go1.26.8，CGO_ENABLED=0，GOMAXPROCS=2。依次运行基线、候选，测量期间没有其他测试/前端构建负载。MySQL测试容器保持空闲，不在本次SQLite微基准路径中。测试使用临时目录中的真实SQLite文件、WAL、synchronous=NORMAL及默认5000ms busy timeout；不是实库MySQL或生产节点测量。

```sh
# 在各自干净检出的 backend 目录分别编译；这里以 baseline.test/candidate.test 表示输出。
CGO_ENABLED=0 GOMAXPROCS=2 go test -p 2 -c -o /tmp/baseline.test ./internal/handler
# 候选检出：
CGO_ENABLED=0 GOMAXPROCS=2 go test -p 2 -c -o /tmp/candidate.test ./internal/handler
# 顺序执行，两端参数完全相同：
GOMAXPROCS=2 /tmp/baseline.test -test.run '^$' -test.bench 'BenchmarkZero(Accounting|EventReduction)$' -test.benchtime=10x -test.count=3 -test.timeout=10m
GOMAXPROCS=2 /tmp/candidate.test -test.run '^$' -test.bench 'BenchmarkZero(Accounting|EventReduction)$' -test.benchtime=10x -test.count=3 -test.timeout=10m
```

实际云端输出 `/tmp/zboard-benchmark-baseline.log`、`/tmp/zboard-benchmark-16815a9.log`，两次 exit0。每模式3个样本、每样本10次操作；这是小样本起点，不能替代长时间负载或显著性检验。基线为 detached 只读工作树，目标开发仍仅一个 codex 分支及Draft PR。

## 结果及限制

以下是每次操作耗时范围/中位数（毫秒）。FullBatch和CoverageOnly每操作32个流量事实；Reduction每操作1024个累计更新、32个flow、1份订阅、1个节点。Reduction的原工具标记SQL/window，实际上这里记录的是整个1024输入操作的SQL总数；window_32包含32个32输入小批，window_2000在本负载中只有1个1024输入批。

| 模式 | 基线范围 / 中位 | 候选范围 / 中位 | SQL/操作（两端相同） |
|---|---:|---:|---:|
| FullBatch | 7.903–9.241 / 9.084 | 9.346–10.420 / 10.236 | 76 |
| CoverageOnly | 0.128–0.191 / 0.136 | 0.196–0.239 / 0.212 | 2 |
| Reduction window_32 | 276.095–293.264 / 282.881 | 296.731–302.244 / 302.225 | 2432，即每32输入小批76 |
| Reduction window_2000 | 13.710–15.423 / 15.356 | 13.915–15.222 / 15.079 | 82 |

候选FullBatch中位数比该组基线高12.7%，CoverageOnly高56.1%，window_32高6.8%，window_2000低1.8%。不把这份结果写成“无退化”或性能提升，也不能仅凭小样本把差异归因到新增财务模型。后续P4应在同一资源预算下交替运行更长样本并定位准备/锁/SQL成本；同批SQL数不变不能证明延迟不变。

候选FullBatch观测3071–3424模拟事件/s，p95为每32输入批10.71–12.82ms（仅10次操作样本），不是核心HTTP接口p95。window_2000的高输入事件率来自同一flow多次累计更新可合并的特定分布，不能用于承诺大量独立用户/连接吞吐。CoverageOnly不是完整结算，不能拿其速率代表计费能力。

FullBatch约1.27MB/op、12834–12840 allocations/op；Reduction window_32约41.5MB/op，window_2000约4.62MB/op。B/op是分配总量，不是RSS、数据库写入字节或本地缓冲增长。本次包含解析/事务/计量及其现有投影SQL，排除夹具/HTTP/持久spool接收、checkpoint fsync、远端停服和真实内核。

尚未采集核心HTTP p95、数据库单次延迟分布、任务积压与接收到结算延迟、CPU/RSS曲线、DB/索引/spool增长、耗尽到停服确认延迟、实际超用、长期混合插件/统计压力或MySQL容量。额度在夹具中足够大，不含真实耗尽停服。完整容量报告需继续运行现有隔离mixed-load与真实公开内核验收，并在业务/计量/存储职责收敛后再评价伸缩收益；不得把本报告当整体P6/P7完成。
