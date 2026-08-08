# Keycloak realm 配置

`realm-eagle.json` 在 Keycloak 启动时经 `--import-realm` 自动导入。

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

`accessTokenLifespan` 设为 900 秒（15 分钟）。刻意保持短寿命：撤销依赖 Redis
黑名单，而黑名单只需保留到 token 自然过期为止。TTL 越长，黑名单占用越久、
被撤销的凭证可用窗口也越长。

### 客户端

| clientId | 用途 | 关键配置 |
|---|---|---|
| `eagle-web` | 管理后台前端（SPA） | 公共客户端，强制 PKCE（S256）。SPA 无法安全保存 secret，因此不发 secret |
| `eagle-system` | 资源服务器 | `bearerOnly`，只验证 token 不发起登录流程 |
| `eagle-worker` | 服务间调用 | 机密客户端，`client_credentials` 授权 |

### audience mapper 是必需的

Keycloak 默认把 access token 的 `aud` 设成 `account`，对资源服务器毫无意义。
两个会签发 token 的客户端都配了 `oidc-audience-mapper`，把 `eagle-system`
写进 `aud`，`config.yaml` 里的 `audience: eagle-system` 校验才有实际作用。

不配这个 mapper 的话，要么校验永远失败，要么只能关掉 `aud` 校验——
后者等于放弃了「这个 token 是发给我的」这层保证。

### 关于 `eagle-worker` 的 secret

`dev-only-worker-secret` **仅供本地开发**。生产环境必须：

1. 从本文件移除 `secret` 字段，让 Keycloak 自动生成
2. 经环境变量或 K8s Secret 注入调用方
3. 定期轮转

留在这里是为了让 `docker compose up` 后无需额外步骤就能跑通端到端测试。
它只在这个一次性的本地 realm 里有效。

### 为什么不预置用户

本文件会进版本库。内置已知口令的账号会一路带到生产环境，这类账号往往
在上线很久后才被发现。

首个管理员请自行创建：

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh create users -r eagle -s username=alice -s enabled=true
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh set-password -r eagle --username alice --new-password 'ChangeMe!'
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --rolename admin
```

### `sslRequired: none`

仅因本地开发走明文 HTTP。**生产环境务必改回 `external` 或 `all`**，
否则 token 会在网络上明文传输。
