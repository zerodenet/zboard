# Zero 节点生命周期规划

## 结论

浏览器 SSH 终端解决的是人工运维入口，协议配置保存解决的是业务期望状态；两者都不能替代 Zero 内核生命周期管理。zboard 需要把节点自动化明确拆成探测、安装、配置、启动、健康确认和回滚，并把每个阶段的真实结果反馈给管理员。

协议保存会触发节点级完整配置发布，不再把单个端点 JSON 写成“暂存成功”。只有 Zero 自身的 `validate`、generation 原子切换、本地 control socket 和新鲜的已认证 Connector 事件全部通过，协议页面才显示配置已生效。

## 状态边界

节点页面分别展示以下状态，不合并成一个“在线”：

1. SSH：未配置、待验证、已验证、主机身份变化。
2. 内核安装：未探测、未安装、安装中、已安装、升级中、失败。
3. 配置：未生成、待应用、校验中、已应用、回滚、失败。
4. 进程：未知、启动中、运行、停止、异常。
5. Zero Connector：按最近一次已认证事件独立判断近期活跃或离线。
6. 计量：上报凭证状态和最近可信上报时间。

## 数据与任务模型

新增一对一的 `node_kernel_states`，保存期望版本、已安装版本、二进制 SHA-256、期望/已应用配置修订、当前阶段、服务管理器、最后健康时间和脱敏错误摘要。

新增只追加的 `node_operations`，记录 `detect`、`install`、`configure`、`repair`、`upgrade` 操作。每次操作保存请求人、状态、阶段、锁定版本/制品、开始/结束时间和结果摘要。每个节点只允许一个内核操作运行；通用批量任务可以调度多个节点操作，但不直接执行 SSH 脚本。可恢复的客户端幂等键仍属于后续能力。

凭证明文不进入操作记录。zboard 使用站点凭证密钥加密保存 Connector 凭证，并只向管理界面暴露前缀；生成的 Zero 配置权限为 `0600`，只把凭证放入 Webhook sink 的 opaque authorization header，不写入操作记录、审计详情或状态输出。

## 安装与升级流程

1. **预检**：通过已验证 SSH 探测操作系统、架构、libc、systemd、系统提权能力、现有 Zero 版本和服务状态。节点可以直接以 root 登录，也可以为普通登录用户显式配置免密/密码 `sudo` 或带独立 root 密码的 `su`；Linux x86_64 + systemd 节点按 libc 选择制品，禁止为了安装内核而升级系统 libc。
2. **锁定制品**：`legacy` 仅在无人值守批量任务中默认选择 `zerodenet/zero` 的最新稳定 Release；单节点操作允许管理员显式选择任意已发布的稳定版或预发布版。glibc ≥ 2.34 使用该标签的 `zero-linux-x86_64.tar.gz`；旧 glibc 优先使用同一 Release 的 `zero-linux-x86_64-musl.tar.gz`，并兼容历史 Release 已发布的 `zero-v<version>-linux-x86_64-musl.tar.gz`。每个压缩包都必须存在引用其精确文件名的同名 `.sha256`。前端只提交版本，后端重新按标签解析发布和固定下载地址，不接受任意 URL。GitHub Release 没有可用 musl 制品时，才回退到 `ZBOARD_ZERO_ARTIFACT_DIR` 中同标签的历史版本化制品。`native-local` 则要求显式 `ZBOARD_ZERO_LOCAL_VERSION`，只读取受信任目录内精确匹配的 `zero-v<version>-linux-x86_64-musl.tar.gz` 和 `.sha256`，不访问或替换为 GitHub Release。所有制品都锁定大小和 SHA-256，不接受跨版本替代或未锁定下载。
3. **暂存与校验**：下载到节点临时目录，核对大小和 SHA-256，执行 `zero version` / `zero build_info`，不覆盖当前版本。
4. **生成完整配置**：把通用 Webhook Connector、磁盘 outbox、控制 socket和全部启用协议端点及原生 managed users 编译为一个规范化 Zero 配置。配置写入版本化 generation 目录并以 `0600` 权限安装。
5. **离线校验**：先运行 `zero validate <staged-config>`。校验失败时不修改二进制、配置软链接或服务。
6. **原子激活**：备份当前二进制和 generation，原子替换二进制及 `current` 配置软链接，安装或更新 `zero.service`，随后 `daemon-reload`、enable、restart。
7. **分层验收**：内核操作依次确认 systemd active、本地 control socket 的 `zero status --json`、配置摘要和激活开始后的新鲜 `stats.sampled` 等已认证 Connector 事件。Connector 活跃仍作为独立状态展示，不与 SSH 或本地进程状态混合，但首次安装/切换只有通过三段验收才算成功。
8. **自动回滚**：任一验收失败，恢复上一二进制和配置 generation，重启并再次健康检查；回滚结果也必须落入操作记录和审计日志。

## 配置发布

协议保存先提交数据库期望状态，随后立即排队发布。zboard 对该节点的全部启用端点和有效订阅凭证生成新的完整配置修订，而不是逐个文件覆盖运行态。新配置先离线校验，再切换 generation 并受控重启；本地健康或 Connector 事件确认失败会恢复上一 generation。协议页的手动操作仅用于重试同一发布链。

安装、配置应用和升级共用同一节点操作锁。这样可以避免在升级二进制时同时发布协议配置，也能让重复请求通过幂等键安全返回同一操作。

## 分阶段交付

- 第一阶段：节点探测、内核状态模型、操作记录和只读 UI，不改变服务器。
- 第二阶段：固定版本首次安装、完整配置生成、systemd 启动、健康验证和自动回滚。
- 第三阶段：配置 generation + validate + 受控重启，把协议 SSH 暂存替换为真实生效流程。（已完成）
- 第四阶段：可控升级/降级、批量调度、灰度和失败节点隔离。

协议页面必须以 desired/applied 哈希、部署结果和节点健康事实展示状态，不得把文件上传显示成“协议已生效”。

## 当前实现（2026-07-26）

已落地 `node_kernel_states`、`node_operations`、节点检测、当前线上稳定版解析、按 libc 选择 official GNU/面板托管 musl 制品、发布包与二进制双重 SHA-256 校验、节点级完整配置生成、`zero validate`、systemd 原子切换、本地 control socket 与 Connector 事件验收和失败回滚。节点页面可以直接检测并执行“安装 / 升级 / 修复 / 配置同步”，同时展示最近操作的真实阶段与错误。

升级判定使用已安装 build ID、实际二进制 SHA-256、期望配置 SHA-256 和本地健康状态：未安装执行安装；版本较旧执行升级；同版本摘要不同执行修复；配置摘要不同执行配置同步；服务或 control socket 异常执行修复。管理员可从稳定发布列表选择精确版本；目标低于已安装版本时，界面必须显示降级语义并二次确认，后端还要求 `allow_downgrade` 与明确版本同时出现。无论选择最新还是历史稳定版，后端都必须重新解析为明确标签、不可变制品 URL 和 SHA-256，随后才允许执行。

当前 musl 制品契约为 `zero-linux-x86_64-musl.tar.gz`，同一 Release 必须包含 `zero-linux-x86_64-musl.tar.gz.sha256`，校验文件内部也必须引用这个精确文件名。为确保已发布版本仍可由管理员指定安装，解析器也接受同一 Release 中历史命名的 `zero-v<version>-linux-x86_64-musl.tar.gz` 及其同名校验文件。旧 glibc 节点不会升级系统 libc；新发布不再依赖历史命名。

节点 SSH 设置已经把登录认证与系统提权拆开：登录仍支持密码/私钥和固定主机指纹；系统命令根据节点配置使用直接 root、`sudo` 或 `su`。提权密码使用站点凭证密钥独立加密，只通过 SSH stdin 提供，不进入远程命令、任务输出和审计详情；普通交互终端仍保持登录用户身份，由管理员自行决定是否在终端内提权。

协议页面保存后的自动 generation 已完成。`ZBOARD_ZERO_KERNEL_CONTRACT=native-local` 时，VLESS、VMess、Shadowsocks、Trojan 和 Hysteria2 凭证都会生成 Zero 原生 managed user，携带稳定 `principal_key`；通用 Webhook Connector 使用完整 URL、opaque authorization header 和磁盘 outbox 发送生命周期及流量事件。默认 `legacy` 保持已发布内核的旧契约，避免 zboard 先行同步后让线上节点收到尚不支持的配置。发布仍使用 `zero validate`、原子软链接、受控重启、control socket、Connector 事件确认和失败回滚。订阅开通、续费、额度调整、额度耗尽与 Connector 活动发现的到期变更都会触发同一发布链。当前 native 契约以本地 Zero 工作区和本地二进制验证，尚未同步到线上或 GitHub release，因此 zboard 发布不等于节点内核已经升级。

仍未完成的是可恢复的远程幂等键、批量/灰度调度和通过 `config.apply` 热更新替代受控重启。Zero 的策略计数按单进程 principal 执行，因此 zboard 只在订阅恰好有一个活跃凭证时下发本地限速和设备数；多节点/多凭证全局限速、设备数和剩余额度仍由面板统一计算，不能把完整额度复制给每个内核。zboard 订阅 `stats.sampled` 作为 Connector 活性信号并更新节点摘要，但它不替代 control socket 的进程健康判断。Shadowsocks 继续使用每凭证独立端口以保持现有订阅地址兼容，但运行态归因已经使用原生 `principal_key`。本地 Zero 的 `MieruUserConfig` 尚无 `principal_key` 或 managed-policy 字段，所以 Mieru 仍使用端点级账号配置，不在本阶段提供原生订阅归因。
