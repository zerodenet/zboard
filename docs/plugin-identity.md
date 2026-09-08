# 插件第三方登录与注册

`zboard.identity.provider.v1` 让 ZBoard 作为第三方 OAuth2 / OIDC 客户端。一个插件可配置多个提供方；参考 OAuth 插件提供 GitHub、Google 快捷配置，以及自定义 OAuth2 / OIDC 配置。提供方之间独立启停，核心登录和注册页分别显示名称。

## 用户路径与核心职责

1. 运营者设置站点的公开 HTTPS 根地址，提供方精确登记 `<站点地址>/api/v1/auth/oidc/callback`，导入可信插件并配置各客户端。
2. 用户点击 GitHub、Google 或自定义入口授权。已有绑定直接登录；新身份由核心处理注册。
3. 新注册遵守核心 `allow_registration` 开关。提供方断言包含有效、已验证邮箱时自动创建普通账户；否则在核心完成页补充邮箱验证码。验证码必需，即使普通密码注册关闭了邮箱验证；邮件服务必须可用。
4. 同邮箱已有账户（包括软删除账户）不会自动合并。用户先通过原方式登录，再确认当前密码显式绑定。一个外部身份只属于一个用户，一个用户可分别绑定多个提供方。
5. 第三方注册账户默认没有本地密码，密码列使用不可登录的哨兵值。用户可在刚完成第三方登录后的五分钟内，于“账户安全”设置初始本地密码。核心同时核验登录用户、一次性授权证明、提供方活动状态和绑定。已有密码不能通过此接口覆盖。

插件没有用户表、角色、权限、订阅、凭证修改或节点命令接口。它只与配置的第三方通信并返回身份断言。核心负责注册策略、邮箱验证、账户唯一性、普通权限、绑定、审计和会话签发。配置保存与检测没有注册副作用。

停用插件或某个提供方、修改配置、升级或卸载会阻止未完成的登录与注册。已提交绑定、已签发核心会话和业务数据不会被删除。解绑即使插件卸载仍可由核心执行，但必须有当前本地密码，防止用户移除唯一登录方式。

## 专用契约

manifest 声明 `zboard.identity.provider.v1`、`zboard.config.v1` 及服务进程。

- `ListIdentityProviders`：返回最多 16 个启用提供方的稳定 ID 和名称，不执行外部网络请求。旧单提供方插件未实现时，宿主回退到插件 ID / 名称。
- `GetIdentityProvider(IdentityProviderRequest)`：按 `provider_id` 返回协议（`oauth2` / `oidc`）、HTTPS Issuer、授权地址、Client ID 和 scopes。空协议兼容旧 OIDC；空 provider_id 兼容旧单提供方绑定。
- `ExchangeIdentity`：接收核心保存的 provider_id、code、精确 redirect_uri、nonce、PKCE verifier 和 expected issuer。只返回 Issuer、Subject、邮箱和严格布尔类型的邮箱验证标志，不返回第三方令牌。
- OIDC 由插件验证签名、issuer、audience、时效、nonce、subject、azp 和存在时的 at_hash。纯 OAuth2 通过令牌调用配置的用户资料接口，按字段映射取得稳定 ID；GitHub 使用数值 ID 和 `/user/emails` 的 primary+verified 邮箱。
- 配置能力新增可选 `DescribeConfig`，返回插件明确投影的公开字段和密钥存在标志。宿主仅在管理员配置通道调用；原始配置仍加密保存。`ValidateConfig` 的 `previous_config_json` 由宿主提供，插件据此实现保留密钥，输出完整标准化配置。配置 revision 的 CAS 和失败回滚仍归宿主。

公开选择键为 `<plugin_id>~<provider_id>`；旧单提供方保留 `<plugin_id>`。绑定表的 `plugin_id` 保存这个选择键，保持 `(user_id, plugin_id)` 唯一约束。绑定主键是发布者、选择键、Issuer、Subject 的结构化字节 SHA-256，避免数据库排序规则合并大小写身份，也隔离不同发布者。此扩展沿用 `0003_external_identities` 表，不增加迁移。

协议 1 增量字段保留 wire 编号；空请求保持旧单提供方行为。新多提供方插件需要本次 SDK / 宿主。旧版本提供页面和配置能力，不代表具备新注册与多提供方能力。

## 状态、事务与端点

核心生成 256 位 state、nonce、PKCE verifier 和浏览器绑定，始终使用 S256。state 五分钟到期且一次消费。HTTPS cookie 使用 `__Host-`、Secure、HttpOnly、SameSite=Lax，固定回调地址，拒绝跨站 POST，令牌不会出现在完成页 URL。

普通完成票据一分钟到期；待注册票据和初始密码证明五分钟到期。换票不延长原始过期时间。邮箱验证码有发送冷却、IP 限额、最多五次验证预算与单次消费；错误尝试独立提交计数，不能通过注册事务回滚重置。验证码失败需要重新授权。

核心完成注册、绑定、审计和签发会话时，插件租约和安装状态检查与身份写入共用数据库事务；再次核验发布者、generation、config_revision。新建用户、绑定和审计任一步失败都会回滚。只在提交成功后尝试加入既有注册欢迎邮件任务。

- `GET /api/v1/auth/oidc/providers`：公开提供方目录。
- `POST /api/v1/auth/oidc/:id/start`：发起授权。
- `GET /api/v1/auth/oidc/callback`：消费 state 并验证身份，跳转固定完成页；迁移维护期禁止此写操作。
- `POST /api/v1/auth/oidc/finish`：登录 / 注册 / 绑定完成；缺少验证邮箱时返回 `registration_required`，随后提交 `email` 和 `verification_code`。
- `POST /api/v1/auth/oidc/registration-code`：持有待注册浏览器票据后发送核心邮箱验证码。
- `POST /api/v1/auth/oidc/password`：登录用户使用刚完成授权的一次性证明设置初始密码。
- `GET /api/v1/account/identities/security`：查询当前账户是否已有本地密码。
- `GET /api/v1/account/identities`：当前用户绑定列表，不返回 Subject 或第三方令牌。
- `POST /api/v1/account/identities/:id/bind`：`:id` 为提供方选择键，需当前用户和密码。
- `POST /api/v1/account/identities/:id/unlink`：`:id` 为绑定记录 ID，需当前用户和密码。

状态在活动插件宿主内存中，每类最多 1024 条；宿主重启需重新授权，多实例入口需路由到活动宿主。真实第三方授权须使用运营者登记的客户端验收；协议 fixture、签名令牌测试或元数据检测不能代替实际账号授权。
