# 网络前置

管理员在「节点与协议 → 网络前置」创建入口，选择入口节点 A 和落地节点 B 的现有协议，配置 A 的监听端口与客户端连接地址。保存、编辑、停用和删除都会进入持久化节点发布队列；失败自动重试，列表展示排队状态与失败原因。

## 权限与订阅

入口不绑定用户、套餐或节点组。B 原有协议先经过订阅权限、有效期、余额、节点状态等过滤，符合条件后才派生 A 前置线路。订阅同时保留 B 直连和「入口名称 · B 协议名称」两条线路。派生线路使用 B 的凭据、节点组和流量倍率；没有 B 权限就没有前置线路。A 无需保存 B 的用户密码或生成用户凭据。

前置线路只替换客户端连接地址和端口，并保留 B 的 TLS、Reality、SNI 与传输参数；原本隐式采用 B 域名的证书校验名称会被显式保留。A 的代理路径凭据只加密存储并下发 A，不回显到管理端，也不下发订阅。

新配置尚未发布成功时，订阅继续提供 B 直连，暂不提供前置线路。A 离线或入口停用时也保留 B 直连。删除入口只撤除 A 转发，不删除 B 或用户权限。删除 A、B 或落地协议之前必须先清理入口引用。

## 运行方式与兼容

A 是固定目标的原始 TCP/UDP 转发器，B 执行协议解密、用户鉴权和用户流量结算。A 的网络统计仍会记录传输量，但不产生 B 用户的第二份扣费。B 看到的网络源地址是转发路径的出口地址。

生成的 Zero 入站复用已有的 `udp.enabled`；全局和入站开关均默认开启：

```json
{
  "tag": "entry-1",
  "listen": {"address": "0.0.0.0", "port": 10000},
  "udp": {"enabled": true},
  "protocol": {
    "type": "direct",
    "target": "landing.example.com", "port": 443
  }
}
```

选择“TCP 与 UDP”时，入口 A 和面板本地的校验内核必须在 `build-info` 的 `protocol_capabilities` 中声明 `direct.inbound.udp.supported=true`。仅能解析 `udp.enabled` 不代表支持原始 UDP 转发；未声明能力时拒绝发布。选择“仅 TCP”时生成 `udp.enabled=false`，保留旧内核可用的 TCP 配置；Hysteria2 不允许选择仅 TCP。仅 TCP 的 Shadowsocks 前置线路在 Clash、sing-box 和 Zero 订阅中明确关闭 UDP，原始 SS 分享链接不能通用表达这个限制，导入后也只能用于 TCP。B 直连线路不受影响。旧内核的 `direct` 只接收 TCP；新内核默认同时监听 TCP 和 UDP。升级后需要仅 TCP 的现有入口应明确关闭 UDP；UDP 端口冲突会导致激活失败并回滚，不能静默降级为 TCP。B 不因前置功能本身需要升级。

目标使用 B 协议的对外地址和对外端口；A 的监听端口与对外端口可以不同，以支持已有端口映射。管理员应确保 A 的 TCP、UDP 端口以及 A 到 B 的路径可达。A 与 B 必须为不同节点，入口端口不能与 A 的协议、用户端口或其他入口重复。历史独立用户端口未迁移完成时拒绝创建，需先重新发布 B 完成统一协议端口迁移。

## 可选代理路径

留空表示 A 直连 B。填写代理路径对象可以选择一个出口、测速组或链式代理组：

```json
{
  "outbounds": [
    {"tag":"hop1","protocol":{"type":"socks5","server":"proxy1.example.com","port":1080}},
    {"tag":"hop2","protocol":{"type":"shadowsocks","server":"proxy2.example.com","port":1080,"cipher":"aes-128-gcm","password":"replace-me"}}
  ],
  "outbound_groups": [
    {"tag":"chain","type":"relay","proxies":["hop1","hop2"]},
    {"tag":"auto","type":"url_test","outbounds":["hop1","hop2"],"interval_seconds":300}
  ],
  "target":"chain"
}
```

`url_test` 在多个候选出口中选择，`relay.proxies` 则按顺序组成多跳代理链，两者含义不同。保存时由 Zero 校验结构、协议字段与引用关系；具体出口还需要支持实际使用的 TCP/UDP 路径。在“TCP 与 UDP”模式下，已知不支持 UDP 的 HTTP CONNECT、VLESS Vision 和以 SOCKS5 为最后一跳的 UDP 代理链会被明确拒绝；仅 TCP 模式允许由内核校验通过的 TCP 路径。每个入口的出口和组标签自动加前缀，相同标签不会在 A 上互相覆盖。

编辑时不提交 `path_config` 保留原路径；提交 `{}` 清除路径、恢复直连。API 使用管理员身份访问 `/api/v1/admin/network-entries`，更新携带当前 `revision` 防止覆盖其他管理员的修改。

前置线路保留 B 的 TLS/SNI/Reality 身份，以及 WebSocket 和 HTTP/2 的 Host；仅连接地址和端口替换为 A，避免长域名、CDN 或证书校验随入口地址发生变化。UDP 为每个客户端建立独立转发会话，避免多个用户向同一 B 协议发送数据时回包混淆。
