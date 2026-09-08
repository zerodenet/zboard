# 面板删除与节点清理

删除面板记录和清理远端机器是两个独立操作。面板删除不连接 SSH、不请求 CA 或供应商 API；过期凭据和网站外部依赖故障不能阻止本地删除。

## 删除范围

| 删除对象 | 数据库内完成的清理 | 远端行为 |
|---|---|---|
| 节点资产 | 节点、协议、用户协议凭据、前置入口、代理池、节点组关联、证书/DNS 管理记录与运行投影 | 不停机、不删除机器文件、不修改供应商 DNS |
| 协议服务 | 协议及凭据、节点组和证书关联、以它为落地的前置入口 | 为相关存活节点排队撤除配置，失败重试 |
| 前置入口 | 入口及节点组关联 | 为 A 排队撤除端口转发，不移除 B 的显式权限 |
| 托管证书 | 面板证书记录、协议关联、面板自动续期 | 保留证书、私钥、Certbot 续期配置，不向 CA 撤销证书 |
| DNS 记录 | 面板管理记录 | 保留供应商的真实解析 |
| 供应商账户 | 面板凭据及关联 DNS 管理记录；解除证书账户关联、关闭自动续期 | 保留供应商账户、Token 和远端资源 |

删除节点 B 会同时移除以 B 协议为目标的前置服务，但保留入口机器 A；A 的撤除配置进入队列。删除 A 不删除 B 或 B 的明确授权。历史流量、订单、订阅、行政任务和审计事实保留，运行配置、凭据与运维明细按其所属资源清理。

数据库关联清理失败时事务回滚。仍在执行的安装、发布、签发、同步任务需要串行完成，避免它们与删除并发重建资源；这与 SSH 是否可达无关。删除后远端旧配置可能继续工作，尤其离线节点不能即时撤销已加载的用户凭据。

## 网站不可用时的停机与卸载

脚本源文件是 `backend/internal/nodecleanup/cleanup-zero-node.sh`，不包含站点地址、Token、SSH 密码，也不需要连接面板或 GitHub。新构建的二进制发布包同时携带 `cleanup-zero-node.sh`。面板的「节点资产 → 内核与运维」提供下载；以后通过面板安装或更新 Zero 时也会安装到 `/usr/local/sbin/zboard-zero-cleanup`。既有节点不会因为本地代码修改而自动获得脚本，需要先下载复制或更新安装。

在 Linux/systemd 节点上使用 POSIX Shell（`/bin/sh`），需要系统常用工具 `systemctl`、`timeout`、`readlink` 等，不依赖 Python 或 Bash。默认只查看状态：

```sh
sh cleanup-zero-node.sh status
```

关闭 Zero 及现有连接，禁止开机自动启动；保留配置、安装文件和待上报事件：

```sh
sudo sh cleanup-zero-node.sh stop --yes
# 已安装到节点时：
sudo /usr/local/sbin/zboard-zero-cleanup stop --yes
```

普通停机最多等 15 秒，仍未停止时向该 systemd 服务发送 KILL；同时检查使用 `/usr/local/bin/zero` 和 `/etc/zerodenet/current.json` 的手动启动进程。不会通过模糊进程名杀掉其他程序。停止或状态核验失败时，脚本返回非零状态，不继续删除文件。

彻底卸载需要明确选择，先停止服务，再清理以下托管路径：

```sh
sudo sh cleanup-zero-node.sh uninstall --yes
```

- `/etc/systemd/system/zero.service` 和同名 drop-in 目录
- `/usr/local/bin/zero`
- `/etc/zerodenet`：环境凭据、当前配置和配置代际
- `/var/lib/zerodenet`：备份与事件 outbox，删除会永久丢弃尚未上报的数据
- `/run/zerodenet`：控制 socket 等运行文件

卸载后保留清理工具本身。`--help` 和卸载完成提示会显示自移除说明。确认清理完成、不再需要工具时，手动删除安装的脚本：

```sh
sudo rm -- /usr/local/sbin/zboard-zero-cleanup
```

手动上传的副本按实际上传路径删除，例如 `rm -- ./cleanup-zero-node.sh`。删除脚本本身不会停止或卸载 Zero。

证书文件默认保留；明确需要时添加 `--certificates`，另外清理 `/etc/zboard/certificates` 和 Certbot 中名称严格匹配 `zboard-数字` 的 live/archive/renewal 资料。不会删除其他证书、共享 ACME 账户、Webroot、系统 Python、SSH、BBR 配置，也不修改 Cloudflare DNS 或在线撤销 CA 证书。

脚本拒绝托管根路径或父目录的符号链接跳转，也拒绝清理配置不属于上述托管实例的 `zero.service`。遇到非标准安装，应先核对真实路径，不要使用全局 `pkill zero` 或删除整个 `/etc/letsencrypt`。

## 面板失联与重试

本地核对的 Zero 源码中，Webhook 单次请求默认 10 秒超时，重试间隔按 4、8、16、32、64 秒退避，上限 64 秒；内存待投递数量默认 4096，outbox 有磁盘剩余空间保留限制。默认可持续重试，因此网站长期不可用时进程及待上报数据会继续存在，但源码并不是无间隔的无限重试循环。

这些是当前本地源码的默认值，不证明远端运行版本、配置或 CPU 状态正常。没有运行现场证据，不能认定连接残留一定会引起 CPU 雪崩。已废弃节点的确定性处理是执行上述停机命令；不应让面板删除依赖能否登录这台机器，也不应把面板暂时断线直接当作卸载全部代理服务的指令。
