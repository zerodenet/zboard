# P4：投影源读取的内存边界

每次恢复仍最多处理200条已结算事实，但以前 SELECT * 会同时载入每条
可变长度诊断 Meta。现在只读取汇总所需的11个身份、时间、方向及计费
字段；不载入 Meta，也不删除或改写原始记录。身份/owner/scope/hour
核对、倍率及已结算字节、两个投影和 checkpoint 的事务保持不变。
没有改变调度、批大小、锁隔离或写入路径。

## 已验证行为

查询后的真实记录证明 Meta 未载入；再次完整读取证明原事实及 Meta
字节保留。Endpoint、user、subscription、node、hour不匹配仍整批拒绝，
不推进投影/checkpoint，修复证据后可恢复。原源读取失败该回归；修复后
全部投影专项及 race 通过。实际MySQL竞争领取/checkpoint失败、小时
失败重建、慢投影不阻核心插入和日历边界四项也通过22.616s。

## 有界测量

Go1.26.8、linux/amd64、GOMAXPROCS=1、-p1，本机cgroup4CPU/16GiB。
没有同时运行其他本地Go/MySQL/Chromium/容量负载。每个样本新建SQLite
文件，200条事实、8个endpoint、2个UTC小时，每条Meta32768字节，总计
6553600字节。计时只包含一个Drain，插入和完整对账在计时外。

| 样本 | 旧 ns/op | 新 ns/op | 旧 B/op | 新 B/op |
|---|---:|---:|---:|---:|
| 1 | 15162492 | 3321139 | 7383080 | 689088 |
| 2 | 11737993 | 3052247 | 7380432 | 681360 |
| 3 | 12742638 | 3133498 | 7380256 | 681472 |
| 4 | 15345906 | 2767130 | 7380256 | 681360 |
| 5 | 13375218 | 3818333 | 7380512 | 681360 |

SQL均为5次/批（不含driver BEGIN/COMMIT），处理及保留事实均200条，
hourly/daily精确相同。中位分配量7380432→681360B；耗时中位数
13.375→3.133ms。先旧后新各五组单批样本，不构成统计置信度、MySQL
耗时、HTTP吞吐、调度时延或普遍容量承诺。原RawTrafficRecord仍是
去重/恢复权威；本批不降低数据库保留量，也不允许归档该权威记录。

复现（backend，环境GOMAXPROCS=1）：

```sh
ZBOARD_BENCH_USAGE_PROJECTION_LARGE_META=1 go test -p 1 \
  ./internal/adapters/persistence/meteringstore -run '^$' \
  -bench '^BenchmarkUsageProjectionLargeMeta200$' -benchtime=1x -count=5 -benchmem
```

旧样本仅用旧production文件overlay，测试/夹具不变。完整原始值、命令、
源码hash及限制见validation/2026-10-20-projection-source-columns.json。
无需迁移；代码回退只会重新载入Meta，不删除历史。当前完整检查、原
HTTP容量门槛复测和P6独立worker仍需另验，无生产操作。
