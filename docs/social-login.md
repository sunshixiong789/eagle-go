# Google / Apple 登录

Eagle 不接收第三方密码。Web 或原生客户端先使用 Google Identity Services、Sign in with Apple JS
或系统 SDK 完成授权，再把 ID Token、登录前生成的原始 nonce 发送给 Eagle。后端会验证第三方
签名、issuer、audience、有效期和 nonce，映射到 provider-independent 的 Eagle 账号，建立本地可撤销
会话，再签发 Eagle 自己的 token。手机号、微信等国内登录应使用各自明确的验证用例，验证成功后复用
同一账号、会话与签发流程，不另建 token 体系。

## 配置

```bash
mkdir -p /run/secrets/eagle-jwt
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out /run/secrets/eagle-jwt/2026-09.pem
export EAGLE_AUTH_SIGNING_KEY_DIRECTORY=/run/secrets/eagle-jwt
export EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID=2026-09
export EAGLE_AUTH_GOOGLE_ENABLED=true
export EAGLE_AUTH_GOOGLE_CLIENT_ID="<google-oauth-client-id>"
export EAGLE_AUTH_APPLE_ENABLED=true
export EAGLE_AUTH_APPLE_CLIENT_ID="<apple-services-id-or-bundle-id>"
```

生产私钥目录必须通过 Secret 只读挂载，不得使用仓库里的开发密钥。文件名去掉 `.pem` 后就是 JWT
header 的 `kid`。轮换时先让目录同时包含新旧密钥并部署，再切换 active key；等待旧 access token
全部过期后才能移除旧公钥。公钥集合可从 `GET /.well-known/jwks.json` 获取。
客户端必须在发起授权前生成至少 128 bit 随机 nonce。Google ID Token 中的 nonce 应与原值一致；
Apple 原生 SDK 常把 SHA-256 后的 nonce 放进 ID Token，后端同时支持原值和十六进制 SHA-256 值。

## 登录

```http
POST /v1/auth/social/login
Content-Type: application/json

{
  "provider": 1,
  "id_token": "<provider-id-token>",
  "nonce": "<original-nonce>",
  "display_name": "<optional-first-login-name>"
}
```

`provider` 使用契约枚举值：Google 为 `1`，Apple 为 `2`。

Apple 只在首次授权时返回姓名，客户端应在首次请求的 `display_name` 中一并提交。服务不会根据邮箱
自动合并 Google 与 Apple 身份，避免 Apple 隐藏邮箱或共享邮箱导致错误接管。新增登录身份先创建独立
Eagle 账号；未来的账号绑定必须要求已登录主体再次验证目标身份。

## 刷新与退出

```http
POST /v1/auth/token/refresh
Content-Type: application/json

{"refresh_token":"<refresh-token>"}
```

每次刷新都会返回新的 refresh token，旧值立即失效。客户端必须原子替换本地保存的 token。

```http
POST /v1/auth/logout
Content-Type: application/json

{"refresh_token":"<refresh-token>"}
```

数据库只保存 refresh token 的 SHA-256 哈希，不保存其明文。access token 是短期 JWT，退出后可能
在剩余有效期内继续可用；敏感客户端应同时清除本地 access token。

默认 access token 有效期为 900 秒。退出只撤销当前会话的刷新能力，不会让已签发 JWT 立即失效；
停用账号或修改 audience 范围内的角色也不会改写已有 JWT。高风险接口可以使用 token 的 `sid` 增加
即时会话检查，普通接口保持本地公钥验签，避免认证中心故障拖垮所有服务。客户端退出时应清除两种 token。
登录和刷新在本地签发成功后才提交数据库事务；签发失败可以使用原 refresh token 重试。
如果事务已提交但响应丢失，原 refresh token 已失效，客户端应重新登录。

## 管理员初始化与账号角色管理

先让目标用户完成一次正常登录，取得返回的 Eagle `subject`。完成数据库初始化后，
在具有该应用数据库权限的运维终端运行同版本命令（镜像内为 `/app/eagle-admin`）：

```bash
make build
export EAGLE_DATABASE_DRIVER=postgres
export EAGLE_DATABASE_DSN='<从部署密钥注入数据库连接>'
export EAGLE_AUTH_AUDIENCE='<与服务配置一致的 audience>'
./bin/eagle-admin -subject '<已有账号 subject>' -actor '<运维人员标识>'
```

每个 audience 只能初始化一次；已有管理员时也拒绝执行，停用账号不能被初始化。
命令在同一事务内写入角色、初始化标记和审计；并发执行只有一次成功。
目标账号刷新或重新登录后获得 `admin`。管理员仍经过 Casbin，权限来自已存在的 `system:*` 策略。
该命令不提供密码重置、身份绑定或绕过数据库认证的功能。

使用管理员 token 管理账号：

| 操作 | HTTP | 权限 |
|---|---|---|
| 分页查询账号 | `GET /v1/system/accounts?page=1&page_size=20` | `system:account:list` |
| 查询角色和版本 | `GET /v1/system/accounts/{subject}/roles` | `system:account:query` |
| 全量替换角色 | `PUT /v1/system/accounts/{subject}/roles` | `system:account:assign` |

PUT 示例（`expected_revision` 使用刚查询到的 revision）：

```json
{"roles":["user","support-agent"],"expected_revision":2}
```

revision 是当前 audience 的全局版本；其他账号变更或首次加入该应用也会使旧版本失效。
遇到 409 必须重新读取并核对，不能盲目重试覆盖。角色名去重排序，`roles: []` 表示撤销所有角色，
之后刷新、退出再登录都不会重新获得默认 `user`；其他 audience 不受影响。
角色名称允许先分配再配置 Casbin 权限，未授予权限的角色不会因此获得接口访问权。
最后一个启用的直接 `admin` 不能被移除；应先给另一启用账号分配管理员。
不要把 `system:account:assign` 授给普通角色，该权限允许分配管理员。

`user_audience` 区分“从未加入应用”和“明确拥有空角色”；`account_role_state` 串行化版本更新，
`account_role_audit` 记录目标、操作者、动作、前后角色和版本。HTTP 操作者取自已验签主体，不能由请求体指定。
角色调整只影响之后签发的 access token，已有 JWT 在其有效期内仍可使用，默认最长 900 秒。
