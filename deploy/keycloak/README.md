# Keycloak realm 配置

`realm-eagle.json` 在 Keycloak 启动时经 `--import-realm` 自动导入。

Keycloak 只是**默认**的本地 IdP，不是唯一选择——见文末「换成别的 IdP」。

## 为什么这个文件里没有注释

JSON 没有注释语法，而 Keycloak 的 `RealmRepresentation` 用 Jackson 的严格模式
反序列化——**任何未知字段都会让整次导入失败**，容器直接以 exit 1 退出：

```
ERROR: Failed to run import
ERROR: Unrecognized field "_comment_ttl" (class RealmRepresentation), not marked as ignorable
```

用 `_comment` 这类键做行内注释在别处常见，在这里行不通。所以说明都写在本文件里。

## 配置说明

### Token 生命周期

`accessTokenLifespan` 设为 900 秒（15 分钟）。资源服务器本地验签，不维护
token 黑名单；会话撤销由 Keycloak 管理，已签发 access token 最迟在
15 分钟后自然失效，因此保持短寿命。

### refresh token 必须一次性使用

```json
"revokeRefreshToken": true,
"refreshTokenMaxReuse": 0
```

**这两项默认是关的，关着就是一个真实的安全缺口。**

默认行为下 Keycloak 每次续期都会下发新的 refresh token，但**旧的仍然有效**。
于是 refresh token 一旦从浏览器存储、日志或代理中泄漏，攻击者可以在整个
`refresh_expires_in` 窗口内反复换取新的 access token，而合法用户毫无察觉。

`eagle-web` 是公共客户端（SPA，无法保存 secret），OAuth 2.0 Security BCP
对这类客户端明确要求 refresh token 一次性使用并具备重放检测能力。开启后，
一个已被用过的 refresh token 再次出现会被判定为重放，Keycloak 直接作废
整个会话——这正是发现令牌被盗的手段。

代价：客户端必须每次都保存续期返回的新 refresh token。用旧的会被拒，
这是预期行为而非故障。

### 客户端

| clientId | 用途 | 关键配置 |
|---|---|---|
| `eagle-web` | 管理后台前端（SPA） | 公共客户端，强制 PKCE（S256）。SPA 无法安全保存 secret，因此不发 secret |
| `eagle-api` | 资源服务器（后端进程） | `bearerOnly`，只验签不签发；client role 构成本服务的角色命名空间 |

### audience mapper 是必需的

Keycloak 默认把 access token 的 `aud` 设成 `account`，对资源服务器毫无意义。
`eagle-web` 配置了 audience mapper 把 `eagle-api` 写进 `aud`，服务端
（`EAGLE_AUTH_AUDIENCE=eagle-api`）才能校验「这个 token 是发给我的」。

不配这个 mapper 的话，要么校验永远失败，要么只能关掉 `aud` 校验——
后者等于放弃了这层保证。

### realm role 与 client role 的区别

两级角色都会参与 Casbin 判定，命名空间不同：

- realm role `admin` → 策略里的 `realm:admin`
- `eagle-api` 的 client role `admin` → 策略里的 `client:eagle-api:admin`

`auth.super_admin_role` 的短路**只认 client role**。同名 realm role 不具备
超管能力——否则任何一个 realm 级的 `admin` 都会顺带拿到本服务的全部权限。

### 为什么不预置用户

本文件会进版本库。内置已知口令的账号会一路带到生产环境，这类账号往往
在上线很久后才被发现。

首个管理员请自行创建：

```bash
docker exec eagle-keycloak-1 /opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin
```

```bash
docker exec eagle-keycloak-1 /opt/keycloak/bin/kcadm.sh create users -r eagle -s username=alice -s enabled=true -s firstName=Alice -s lastName=Admin -s email=alice@example.com
```

```bash
docker exec eagle-keycloak-1 /opt/keycloak/bin/kcadm.sh set-password -r eagle --username alice --new-password 'ChangeMe!'
```

```bash
docker exec eagle-keycloak-1 /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --rolename admin
```

最后一条给的是 realm role `admin`，对应迁移 `00003_seed.sql` 里种下的
`realm:admin` 策略。想让 alice 走超管短路，改为授予 client role：

```bash
docker exec eagle-keycloak-1 /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --cclientid eagle-api --rolename admin
```

### `sslRequired: none`

仅因本地开发走明文 HTTP。**生产环境务必改回 `external` 或 `all`**，
否则 token 会在网络上明文传输。

## 换成别的 IdP

本服务是纯粹的 OIDC 资源服务器：只用 JWKS 验签 + 读 claim，不建用户表、
不做 OIDC discovery、不依赖任何 Keycloak 专有接口。换 IdP **不改代码**，
只调下面这几项配置（对应 `configs/config.yaml` 的 `auth` 段，
也可用 `EAGLE_AUTH_*` 环境变量覆盖）：

| IdP | `jwks_path` | `realm_roles_claim` | `client_roles_claim` |
|---|---|---|---|
| Keycloak | 留空（默认 `/protocol/openid-connect/certs`） | 留空（`realm_access.roles`） | 留空（`resource_access`） |
| Logto | `/oidc/jwks` | `roles` | `roles` |
| Auth0 | `/.well-known/jwks.json` | `https://<你的命名空间>/roles` | 同左 |
| Authing | `/oidc/.well-known/jwks.json` | `roles` | `roles` |

配套的三项：

- `issuer` 必须与 token 里的 `iss` **逐字相同**（含协议、端口、尾斜杠）
- `audience` 填该 IdP 里本服务的 API identifier / resource indicator
- `client_id` 决定从 client 角色里取哪个命名空间；Auth0 / Logto 这类把角色
  平铺成一维数组的 IdP，该数组会整体当作本 client 的角色

苹果、Google 等第三方登录也在 IdP 侧配置（Keycloak 的 Identity Provider、
Auth0 的 Social Connections），应用只管拿到的那张 token，同样不需要改代码。
