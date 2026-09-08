# 插件开发与运维

本期提供插件市场、插件管理、离线导入、三个前后台页面范围，以及可选服务端配置进程和身份提供方登录。核心业务所有权见 [设计](plugin-system-design.md)。

## 配置宿主

在现有 ZBoard YAML 配置中添加：

```yaml
plugins:
  directory: /var/lib/zboard/plugins
  catalog_url: ""
  trusted_publishers:
    example.publisher: "BASE64_ED25519_PUBLIC_KEY"
```

公钥为 32 字节 Ed25519 公钥的标准 Base64 文本。默认没有可信发布者，因此无法安装包；只配置经过运营者确认的公钥。目录默认是当前工作目录的 `data/plugins`，应显式配置为持久卷中的独立目录。支持 `ZBOARD_PLUGIN_DIRECTORY`、`ZBOARD_PLUGIN_CATALOG_URL` 覆盖；可信发布者通过配置文件维护，修改后重启宿主。

安装、启停及配置插件无需重启 ZBoard。一个数据库只允许一个活动插件宿主。其他 ZBoard 实例保留核心服务，但不会获得插件执行或页面会话资格；停机切换后重启接管实例，意外退出需先等待最多一分钟租约到期。插件运行目录或初始化失败只使插件接口不可用，不停止核心控制台。备份数据库、插件目录和既有凭证加密密钥。使用容器时把插件目录放在持久卷内，服务插件二进制需要匹配容器平台并具备执行权限。

服务插件为受信任原生代码；进程隔离不等于 OS 沙箱。该版本不提供任意第三方二进制的安全执行环境，也不向插件开放核心业务写入能力。

## 管理流程

能力准入、私有存储、升级迁移与清除策略由宿主保证，见 [插件宿主生命周期](plugin-governance.md)。管理页只展示范围、状态和结果，没有人工授权或手工迁移步骤。

后台“扩展中心”包含插件管理和插件市场。插件管理列表用于搜索、筛选和启停；“查看详情”进入独立页面，按“概览 / 版本管理 / 操作记录”查看信息，操作记录每页展示 10 条最近记录。列表筛选保存在 URL 中，浏览器返回时恢复。

“配置插件”进入独立配置页，优先显示插件自带的配置界面；“高级 JSON 配置”在弹窗中编辑完整配置，保存使用打开弹窗时读取的配置修订号，避免覆盖并发变更。离开有未保存内容的 JSON 编辑器时会提示确认。

离线导入在弹窗中选择签名 `.zbplugin` 文件，再点击“验证并导入”，不会在选择文件时安装。成功后进入插件详情；首次安装保持停用，升级保持原启停状态。失败信息留在弹窗中供修正、重试。文件最大 32 MiB；展开最多 64 MiB、512 个内容文件。签名或结构校验失败不会执行插件；候选运行时校验失败不会提交安装或切换现有实例。

导入时由宿主自动检查兼容性、发布者和范围，完成数据初始化或迁移。首次安装保持停用，必要时保存业务配置后启用。纯页面插件不启动服务进程。服务插件测试使用已保存配置；停用状态下测试临时启动诊断进程，测试完成即退出。

升级直接导入同 ID、同发布者的新版本，宿主准备候选数据和进程，成功后原子切换并保留原启停状态；失败则保留旧版本、数据和实例。未声明测试当前宿主的升级包要求先停用。详情中可在停用后切换已保留版本，宿主检查数据及已有配置，切换后保持停用。每插件最多 30 个不同包版本，安装记录最多 200 条，包括卸载墓碑；当前没有自动历史清理接口。

卸载由宿主停止运行、撤销会话并删除程序与页面，保留配置、私有数据和记录。详情中的“清除保留数据”仅在卸载后可用，统一清理该插件的配置、私有数据及版本状态，不影响核心用户、身份绑定、订单或凭证。再次安装需重新导入包。配置 JSON 完整替换旧配置，秘密值不回显；请保留安全的原始配置来源。

## 创建并签名离线包

从仓库根目录运行，需要仓库要求的 Go 工具链：

```sh
mkdir -p /tmp/zboard-plugin-demo
go -C backend run ./tools/pluginpackager -keygen /tmp/zboard-plugin-demo/publisher.key
go -C backend run ./tools/pluginpackager \
  -source ../examples/plugins/welcome \
  -key /tmp/zboard-plugin-demo/publisher.key \
  -key-id example.publisher \
  -out /tmp/zboard-plugin-demo/welcome.zbplugin
```

将 `publisher.key.pub` 的内容加入宿主可信发布者配置。私钥留在发布者机器，不上传 ZBoard，不放进插件源目录。示例在 public/account/admin 各贡献一个页面，用于验证动态导航、资源加载及宿主上下文桥。包源目录只允许 `manifest.json`、`ui/` 和 `runtimes/` 中的普通文件；打包器生成文件摘要与 `signature.json`。

manifest 示例见 [welcome/manifest.json](../examples/plugins/welcome/manifest.json)。各字段：

| 字段 | 含义 |
| --- | --- |
| schema_version | 固定为 1 |
| id/version | 稳定插件 ID / 严格 SemVer |
| requires.zboard | 必须满足的宿主版本范围 |
| requires.tested_zboard_versions | 发布者明确验证过的宿主版本 |
| requires.plugin_protocol / ui_bridge | 当前均为 1 |
| capabilities | 接受 zboard.ui.page.v1、zboard.config.v1、zboard.identity.provider.v1、zboard.storage.v1 |
| surfaces | public、account、admin 中的子集；服务端插件可为空 |
| components.ui | 各范围对应的 ui/ 内 HTML 入口 |
| components.server.executables | 平台到 runtimes/ 内二进制的映射，例如 linux-amd64 |
| contributions.pages | 页面 ID、标题、surface，以及 business 或 configuration 用途 |
| files | 所有内容文件的 SHA-256；由打包器生成 |

配置页面必须声明 admin 和 config 能力。未知能力或字段、路径穿越、软链接、重复路径、未声明文件和摘要不一致均拒绝。签名覆盖包中 manifest 的原始字节，修改空白也需重新签名。包内不接受独立目录条目，使用仓库打包器生成标准 ZIP。

## 页面桥

iframe 从 URL fragment 读取 `bridge_token`，通过 `parent.postMessage` 发送：

```js
parent.postMessage({
  source: 'zboard-plugin-ui',
  bridge_token,
  request_id: 'context-1',
  type: 'context.load'
}, '*')
```

宿主响应 `source: 'zboard-plugin-host'`、同一 bridge_token/request_id、`ok` 以及 `result` 或通用 `error`。插件也应校验 `event.source === parent` 和桥令牌。可发送 `ui.resize`（height 320–1000）及 `plugin.ready`。并发最多 8 个请求，ID 最多 80 个字母/数字/下划线/连字符，配置最长 64 KiB。

`context.load` 仅返回当前 plugin_id、page_id 和 surface；configuration 用途页面额外支持 config.load、config.save（revision/config）、config.test。config.load 只返回 revision/configured。iframe 没有宿主登录令牌，也不能请求网络、提交表单、访问宿主 DOM 或导航顶层窗口。JS/CSS/图片应放在包内并用相对路径引用。

每个资源会话有效十分钟。页面关闭时撤销，停用或版本变化后立即拒绝后续请求；已打开容器通过每十五秒的可见性检查移除失效页面。会话过期后使用“重新加载”。

## 服务端 SDK

公开 Go SDK 位于 `backend/pkg/pluginapi/v1`，协议源是 `control.proto`。服务实现 `PluginControlServer` 的 GetInfo、Health、ValidateConfig、ApplyConfig、TestConfig，用 `pluginapi.Serve` 启动。可运行实现见 [测试服务](../backend/internal/plugins/testdata/server/main.go)。

宿主使用 go-plugin gRPC/mTLS，限制 RPC 报文和启动/配置超时；每次从已验签包复制当前平台二进制到独立工作目录，不继承宿主环境。GetInfo 必须与 manifest 的 ID、版本和能力一致。配置验证应返回规范化 JSON 对象，ApplyConfig 必须以 revision 幂等，禁止在配置应用或测试中执行业务扣款、修改核心凭证等副作用。标准输出和错误不会直接进入管理页面。

该 SDK 当前没有宿主业务回调、通用命令、事件订阅或 KV API；需要业务扩展时先在核心设计专用能力，未开放能力不能通过自定义方法绕过。

身份提供方插件额外实现 ListIdentityProviders、GetIdentityProvider 和 ExchangeIdentity。完整注册、绑定、回调与核心会话契约见 [插件身份提供方](plugin-identity.md)。认证能力不向 iframe 桥开放。

## 提供市场目录

将插件包发布到公开 HTTPS 的 443 端口地址。市场地址与下载地址不允许查询参数、重定向、内网地址或环境代理。目录内容示例：

```json
{
  "schema_version": 1,
  "expires_at": "2030-01-02T00:00:00Z",
  "entries": [{
    "id": "example.welcome",
    "name": "欢迎卡片",
    "description": "前后台页面示例",
    "version": "1.0.0",
    "publisher": "example.publisher",
    "package_url": "https://plugins.example.com/welcome.zbplugin",
    "sha256": "REPLACE_WITH_PACKAGE_SHA256",
    "surfaces": ["public", "account", "admin"]
  }]
}
```

将有效期改为发布时刻之后、最多 31 天内的 UTC 时间，摘要替换为整个 `.zbplugin` 的 SHA-256。每个 ID 一条目录记录，最多 200 条。目录签名者与包发布者可不同，但各自公钥都必须受宿主信任。

```sh
go -C backend run ./tools/pluginpackager \
  -catalog /tmp/zboard-plugin-demo/catalog-payload.json \
  -key /tmp/zboard-plugin-demo/publisher.key \
  -key-id example.publisher \
  -out /tmp/zboard-plugin-demo/catalog.json
```

打包器输出 `{payload, signature}`，对紧凑 payload 的精确原始字节签名。发布该文件并设置 `plugins.catalog_url`；不要通过再格式化或转义修改 payload。宿主校验目录有效期与签名，安装时再次校验包，旧页面选择的摘要过期会返回冲突并要求刷新。

没有配置目录时市场显示空状态，离线导入仍可用。本仓库不预置公共插件源，也不包含在线开发者上架服务。

第三方登录与注册、多提供方快捷配置、自定义 OAuth2 字段映射和配置密钥保留契约见 [插件身份能力](plugin-identity.md)。配置读取可包含插件投影的公开字段；密钥保持隐藏。
