# 插件身份提供方

`zboard.identity.provider.v1` 支持“第三方账号登录已有 ZBoard 账号”。提供方插件负责 OIDC/OAuth 通信和身份验证，核心拥有账号绑定、登录状态和会话。插件 SDK 只返回 Issuer 和 Subject，没有用户 ID、邮箱自动匹配、角色、权限、数据库、凭证修改或节点命令。

## 使用路径

1. 运营者在“站点与品牌”配置正确的公开访问地址，生产环境使用 HTTPS 根地址。提供方登记精确回调 `<站点地址>/api/v1/auth/oidc/callback`，不能依据请求 Host 或 X-Forwarded-Host 临时生成。
2. 导入可信身份提供方插件，保存其完整配置并启用。后台配置页通过原有沙箱桥管理设置；登录和回调在核心页面执行，不放开 iframe 的网络、表单或顶层导航权限。
3. 用户先通过现有邮箱密码登录，在“账户安全”输入当前密码并绑定第三方账号。绑定回调再次检查账号状态和密码摘要；一个第三方身份不能绑定到两个用户，每个用户对一个插件最多保留一个绑定。
4. 此后在登录页点击第三方提供方即可登录。未绑定身份不会创建用户，也不会按相同邮箱关联账号；新用户仍走现有注册策略。绑定和解绑均记录核心审计日志。

停用或升级插件、修改提供方配置会阻止未完成的登录，不会删除已提交绑定、现有 ZBoard 会话或业务数据。用户可确认当前密码后解绑，即使插件已经卸载。保留邮箱密码入口作为管理和账户恢复路径。

## 专用契约

manifest 必须声明 `zboard.identity.provider.v1` 与 `zboard.config.v1`，并有服务端可执行文件。新增 RPC 是向后兼容的协议 1 扩展，旧宿主会因未知能力拒绝安装。

- `GetIdentityProvider`：返回 HTTPS Issuer、授权端点、Client ID 与 scopes，不返回 Client Secret。
- `ExchangeIdentity`：接收核心保存的 code、精确 redirect_uri、nonce、PKCE verifier 与 expected issuer。插件交换授权码，验证 ID Token 签名、Issuer、Audience、时效、nonce、subject、azp，以及存在时的 at_hash，只返回验证后的 Issuer / Subject。
- `ApplyConfig` 和 `TestConfig` 继续没有用户、身份绑定或登录副作用。发现检测通过不等于客户端密钥有效或真实授权成功。

核心将发布者、插件 ID、Issuer 和 Subject 的结构化字节做 SHA-256 作为绑定主键，避免 MySQL 排序规则折叠大小写，也防止插件 ID 在更换发布者后继承旧绑定。绑定记录由核心 `external_identities` 表保存，MySQL 增量迁移为 `0003_external_identities`，SQLite 同步登记并纳入跨数据库迁移清单。

## 状态与事务

核心生成 256 位随机 state、nonce、PKCE verifier 和浏览器 cookie，授权 URL 使用 S256。state 只消费一次、五分钟到期；HTTPS cookie 使用 `__Host-` 前缀、Secure、HttpOnly、SameSite=Lax，拒绝跨站 POST 和表单内容类型。回调只允许固定核心地址，校验提供方返回的 iss（若有），不把 Token 或授权码附在完成页 URL。

回调成功后通过一分钟、单次消费的 HttpOnly cookie 交付完成结果，核心前台 POST `/auth/oidc/finish` 才获得现有格式的 ZBoard 用户/会话响应。消费时再次检查绑定存在、账号 active、插件发布者/generation/config_revision 和活动宿主租约。租约与安装行锁和核心身份写入处于同一数据库事务，阻止停用、配置变化或另一宿主接管跨过提交边界。已开始的提供方网络交换可以失败，失败不签发会话。

流转状态保存在处理请求的宿主内存，每类最多 1024 条，到期按请求清理。宿主重启后用户需重新发起；多实例入口必须将插件认证流量路由到活动插件宿主，本期不提供分布式登录状态。回调 GET 具有写入语义，核心维护中间件在数据库迁移期间同样禁止它。

## 端点

- `GET /api/v1/auth/oidc/providers`：公开显示活动提供方的 ID/名称；插件不可用返回空数组，邮箱登录独立可用。
- `POST /api/v1/auth/oidc/:id/start`：创建浏览器登录流，返回授权 URL。
- `GET /api/v1/auth/oidc/callback`：验证并消费流转状态，处理插件身份结果，重定向到固定完成页。
- `POST /api/v1/auth/oidc/finish`：消费结果并签发核心会话；绑定操作只返回 `linked=true`。
- `GET /api/v1/account/identities`：读取当前用户的绑定，不返回 Subject 或提供方 Token。
- `POST /api/v1/account/identities/:id/bind`：`:id` 为插件 ID，需当前用户 Token 和密码确认。
- `POST /api/v1/account/identities/:id/unlink`：`:id` 为绑定 ID，只能删除当前用户自己的绑定，需密码确认。

## 验证边界

核心测试覆盖先绑定后登录、真实核心 Token 可用、未绑定拒绝、账号停用、跨浏览器 cookie、状态/结果重放、配置失效、来源校验与密码确认。真实 gRPC 进程测试覆盖新增能力、配置变更和停用后的提交拒绝。OAuth 插件独立测试使用签名 ID Token 验证协议，不需要真实第三方账号。生产提供方仍需使用运营者登记的 Client ID、Secret 和回调完成真实授权验收。
