# 底座怎么用

这份文档写给刚转到 Go、第一次改这个仓库的人。
读完你应该能独立做四件事：**加一个接口、给它配权限、授给某个角色、在代码里拿到当前登录人**。

还没把服务跑起来的，先走 [GETTING_STARTED.md](../GETTING_STARTED.md)。
想知道「为什么这样分层」再看 [README.md](../README.md) 和 [architecture.md](architecture.md)。

---

## 1. 底座已经替你做了什么

你**不用**自己写这些：

| 你想做的事 | 底座已经有了 | 你要写的 |
|---|---|---|
| 判断请求是谁发的 | Keycloak 发 token，`pkg/authn` 本地验签 | 无 |
| 判断能不能调这个接口 | proto 上声明权限，中间件自动拦 | 在 proto 上标 `access` / `perm` |
| 给角色配权限 | `PUT /v1/system/role-bindings/{role}` | 调接口或改种子数据 |
| 前端菜单、按钮显隐 | `GET /v1/system/permissions/me/menus`、`.../me/permissions` | 把权限码挂到导航节点上 |
| 下拉框选项 | 字典模块 | 在后台维护字典项 |
| 写业务接口 | HTTP + gRPC 同一份 proto 生成 | 按下面的分层抄字典模块 |

你**不要**做这些：

- 不要自己建用户表、存密码。用户只在 Keycloak。
- 不要在 handler 里写 `if 没权限 then 403`。权限写在 proto 上。
- 不要在管理端「先建一个按钮，再编一个权限码」。码必须先进入权限目录。
- 不要把「这个人是什么角色」再存一份到本库。角色跟 token 走。

---

## 2. 先记住三件事

一次请求只问三个问题：

```
Keycloak          本服务中间件              本库 Casbin
你是谁？    →     token 是否有效？    →    你的角色能不能做这件事？
你有哪些角色？     这个接口要什么权限？
```

- **角色**（`admin`、`user`）在 Keycloak 里授给用户，出现在 token 里。
- **权限码**（`system:dict:add`）写在 proto 上，也存在本库的 `permission_definition` 表。
- **角色 → 权限码** 的对照表在本库，后台可以改，改完各副本会重载。

权限码只有两种合法写法：

- 具体码：`系统:资源:动作`，三段，例如 `system:dict:add`
- 授给角色的通配：只能末段是 `*`，例如 `system:dict:*`、`system:*`

`system:*:add` 这种中间带星的会被拒绝——写的人以为只放开「新增」，实际会被引擎当成整个 `system` 域。

---

## 3. 一次请求实际走过哪里

以 `POST /v1/system/dict/types` 为例（新建字典类型，需要 `system:dict:add`）：

1. HTTP 进 Kratos，中间件从左到右执行。
2. `authn` 验 JWT，把登录人放进 `context`（`pkg/identity`）。
3. `authz` 读 proto 上的 `access` + `perm`，拿 token 里的角色问 Casbin。
4. 通过之后才进入 `service` → `biz` → `data`。
5. 你的业务代码里**不要**再判一次权限。需要「当前是谁」时，从 context 取。

中间件判定顺序（记这个就够排 401/403）：

1. 声明了公开访问 → 放行
2. 没登录 → **401**
3. 只要登录、不要具体权限 → 放行
4. 本服务的超管 client 角色 → 放行
5. 其余按角色查 Casbin，没有对应权限 → **403**

漏写 `access` 的新接口，服务**启动就会失败**。这是故意的：忘了标权限，不能默默变成「登录就能调」。

---

## 4. 任务：加一个需要权限的接口

这是你以后做得最多的事。下面用「给字典加一个按类型导出的接口」当例子。
你不必真的加导出，照这个顺序改自己的接口即可。

### 第 1 步：在 proto 上声明接口和权限

打开 `api/eagle/system/v1/dict.proto`，在 `service DictService` 里加：

```protobuf
rpc ExportDictData(ExportDictDataRequest) returns (ExportDictDataResponse) {
  option (google.api.http) = {get: "/v1/system/dict/data/export"};
  option (eagle.annotations.v1.perm) = "system:dict:query";
  option (eagle.annotations.v1.access) = ACCESS_LEVEL_PERMISSION_REQUIRED;
}
```

三个 option 的意思：

| option | 作用 |
|---|---|
| `google.api.http` | 生成 HTTP 路由。你用 curl 调的是这个 |
| `perm` | 调用需要哪条权限码。必须是严格三段 |
| `access` | 访问级别，见下一小节。新接口**必须**写 |

`access` 只能选一个：

| 值 | 谁能调 | 能不能再写 `perm` |
|---|---|---|
| `ACCESS_LEVEL_PUBLIC` | 任何人 | 不能 |
| `ACCESS_LEVEL_AUTHENTICATED` | 登录即可 | 不能 |
| `ACCESS_LEVEL_PERMISSION_REQUIRED` | 登录且有对应权限 | 必须写合法的 `perm` |

「查我自己的菜单」这类接口用 `AUTHENTICATED`。
业务写操作一律 `PERMISSION_REQUIRED`。

### 第 2 步：把权限码写入目录

权限码不能只写在 proto 里。启动时服务会核对：**proto 上用到的每一条 `perm`，都必须出现在 `permission_definition` 表里**。对不上，进程起不来。

已有的码（`system:dict:query`）种子数据里已经有了，这一步可以跳过。

**新码**要加一条 goose 迁移。在 `db/migrations/` 下新建文件，编号接当前最大号：

```sql
-- +goose Up
INSERT INTO permission_definition (code, service, resource, action, status, source)
VALUES ('system:notice:add', 'system', 'notice', 'add', 1, 'migration');

-- +goose Down
DELETE FROM permission_definition WHERE code = 'system:notice:add';
```

`service` / `resource` / `action` 就是权限码按冒号拆开的三段。
本地执行：

```bash
goose -dir db/migrations postgres "postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable" up
```

不要在「创建菜单」的接口里发明新码。导航只能引用目录里已经启用的码。

### 第 3 步：生成代码

```bash
buf generate --template buf.gen.yaml
```

会更新 `api/eagle/system/v1/dict.pb.go`、`*_http.pb.go`、`*_grpc.pb.go`。
这些文件不要手改。

### 第 4 步：补 service / biz / data

对照已有的 `ListDictData` 抄一遍：

1. `app/system/internal/service/dict.go`：把 proto 请求转成领域对象，调 usecase
2. `app/system/internal/biz/dict.go`：编排（字典这种 CRUD 往往就是一行 `return repo.Xxx()`）
3. `app/system/internal/data/dict.go`：真正查库

handler 里不要出现鉴权 `if`。需要当前用户时：

```go
import "github.com/eagle-go/eagle/pkg/identity"

p, ok := identity.FromContext(ctx)
if !ok {
    // 正常走不到这里：要登录的接口在中间件就被 401 了
}
// p.Subject   Keycloak 的用户 id（UUID 字符串，不是自增数字）
// p.Username  登录名，适合打日志
// p.Roles     用来展示，不要在这里再做一遍鉴权
```

只想要用户 id：

```go
id := identity.Subject(ctx) // 服务账号或未登录时是空串
```

### 第 5 步：注册到服务器（仅当新加了一个 Service）

往已有的 `DictService` 里加 RPC，**不用**改 `http.go` / `grpc.go` / `wire.go`。
生成代码已经挂在同一个 service 上了。

只有当你新建了 `XxxService`（一个新的 proto `service`）时，才需要：

- `internal/server/http.go`、`grpc.go` 里 `RegisterXxxService...`
- 若有新的构造函数，改 `wire.go` 后执行 `cd app/system/cmd/server && wire`

### 第 6 步：跑测试

```bash
go test ./app/system/internal/domain ./pkg/authz          # 很快，不启数据库
go test ./app/system/internal/data ./app/system/internal/e2e
```

---

## 5. 任务：把权限授给某个角色

角色在 Keycloak 里创建并授给用户。本库只保存「这个角色能做什么」。

给 `user` 角色加上 `system:dict:query`（需要你自己先有 `system:role:assign` 权限，超管用 `admin` 即可）：

```bash
curl -s -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"permission_codes":["system:dict:query","system:dict:list"]}' \
  http://127.0.0.1:8000/v1/system/role-bindings/user
```

这是**全量覆盖**：请求体里没有的码会被收回。改完当前副本立即生效，其它副本靠 Redis 通知，最多几秒对上。

常用查询：

```bash
# 已配置过权限的角色
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/v1/system/role-bindings

# 当前登录人展开后的权限码（前端做按钮显隐）
curl -s -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8000/v1/system/role-bindings/me/permissions

# 当前登录人能看见的菜单
curl -s -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8000/v1/system/permissions/me/menus
```

`admin` 在配置里是超管（`auth.super_admin_role`）。它认的是 **本服务 client 角色**，不是 realm 里碰巧同名的角色。本地开发给用户加的 `admin` 是 realm 角色，种子策略里同时给 `admin` 配了 `system:*`，所以即使不走超管短路也能调通。

---

## 6. 任务：给前端挂一个菜单或按钮

导航和授权是两张表：

- `permission_definition`：后端契约，「有没有这条权限」。
- `navigation_node`：前端树，「菜单长什么样」。

隐藏或删除菜单，**不会**删掉后端权限。反过来，建菜单也**不能**创造出新权限。

按钮必须带一个目录里已有的具体码，例如 `system:dict:add`。
目录、菜单可以不带码。

需要菜单时，用已有的权限树接口创建节点，`code` 填目录里的码。
填一个不存在的码，接口会返回「权限码不在权限目录中」。

---

## 7. 任务：加一整块 CRUD

照抄字典，不要发明新骨架。建议按这个顺序改文件：

1. `ent/schema/` 里用 Go 描述表字段
2. `go generate ./ent`（只改 `ent/schema/`，不要手改生成文件）
3. `db/migrations/` 写 SQL（真正建表靠 goose，**不用** ent 自动迁移）
4. `api/eagle/system/v1/*.proto` 定义接口，每个 RPC 写 `access` + 需要时写 `perm`
5. `buf generate --template buf.gen.yaml`
6. 新权限码写入 `permission_definition`（见第 4 节第 2 步）
7. `internal/domain/` 放对象和仓储接口。字典这种没有复杂规则的，做成普通结构体即可
8. `internal/data/` 实现仓储，这里才能 import `ent`
9. `internal/biz/` 编排用例
10. `internal/service/` 做 proto ↔ 领域对象转换，错误不用你转，中间件会把领域错误映射成 HTTP/gRPC
11. 新的 proto `service` 才去改 `server` + `wire`

有「必须守住的规则」时（权限码格式、树不能成环、按钮必须有码），把规则写进 `domain`，用测试锁住：

```bash
go test ./app/system/internal/domain/...
```

没有这种规则时，不要为了「看起来像 DDD」去造聚合根。

---

## 8. 代码该放哪一层

依赖只能从外往内：`server → service → biz → domain ← data`。

用人话：

| 目录 | 放什么 | 类比 |
|---|---|---|
| `api/` | 接口长什么样 | 接口文档的源文件，改完要生成代码 |
| `service/` | proto 和内部对象互转 | Controller 里「接请求、调应用服务」那几行 |
| `biz/` | 先做什么再做什么 | 应用服务 / 用例 |
| `domain/` | 规则本身 | 和数据库、HTTP 无关的业务语言 |
| `data/` | 怎么存怎么读 | Repository 实现 |
| `pkg/` | 每个服务都会用的技术能力 | 验签、鉴权中间件、DB/Redis。**禁止**依赖 `app/` |

拿不准时问一句：这是「所有服务都要用的技术能力」，还是「某个业务自己的事」？
前者进 `pkg`，后者进 `app/system`（或以后的 `app/订单`）。

当前只有一个进程、一个 `system` 服务。新业务先继续加在这个仓库的 `app/` 下，不要一上来拆第二个可部署服务。

---

## 9. 改完检查清单

- [ ] 每个新 RPC 都写了 `access`
- [ ] 需要权限的接口，`perm` 是严格三段，并且已插入 `permission_definition`
- [ ] 改了 proto 后跑过 `buf generate --template buf.gen.yaml`
- [ ] 改了 `ent/schema/` 后跑过 `go generate ./ent`，并手写了 goose 迁移
- [ ] 没在 handler 里手写鉴权
- [ ] 没把用户名/密码存进本库
- [ ] `go test ./...` 能过

常用命令：

```bash
make init          # 第一次：安装 buf / wire / goose
make generate      # proto + 配置 + wire
go generate ./ent  # 只在改了 ent/schema 之后
make migrate-up    # 执行迁移
go test ./...      # 不需要 Docker
go run ./app/system/cmd/server -conf app/system/configs
```

---

## 10. 从别的语言转过来，最容易做错的

1. **在业务方法里鉴权。** 这里等价物是 proto 注解 + 中间件，不是方法上的 `if`。
2. **自己做登录。** 登录页、改密码、用户列表都不在本服务，去 Keycloak。
3. **用自增 id 当用户主键。** 用户锚点是 token 里的 `sub`（UUID 字符串）。
4. **改完 proto / schema 忘了生成。** Go 不会监视这些文件。CI 会检查生成结果是否脏。
5. **配置里写 `30m`、`1h`。** 时长只认 `3600s`、`0.5s`。
6. **看网上 Kratos v2 教程。** 本项目是 v3，日志是标准库 `log/slog`，没有 `log.Helper`。
7. **给角色授一个目录里没有的码。** 绑定接口会拒；就算绕过去，也匹配不到任何 RPC。
8. **把 `pkg` 写成业务兜底包。** 菜单、字典、某张订单表都不能进 `pkg`。
