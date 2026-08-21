# 新手上手指南

这份文档假设你是第一次接触这个项目，甚至第一次接触 Go 微服务开发。
目标是让你能把服务跑起来、成功调通一个接口。

服务跑通之后，日常怎么加接口、配权限、拿当前登录人，看
[docs/usage.md](docs/usage.md)（按刚转到 Go 的人写的）。
已经熟悉 Go / Kratos、想看设计取舍，再读 [README.md](README.md)。

---

## 目录

1. [这个项目是做什么的](#1-这个项目是做什么的)
2. [开始之前：几个会反复出现的名词](#2-开始之前几个会反复出现的名词)
3. [环境准备](#3-环境准备)
4. [手把手跑起来](#4-手把手跑起来)
5. [跑测试](#5-跑测试)
6. [项目目录导览](#6-项目目录导览)
7. [接下来：怎么用这个底座](#7-接下来怎么用这个底座)
8. [常见问题 / 踩坑 FAQ](#8-常见问题--踩坑-faq)
9. [想深入了解，看这里](#9-想深入了解看这里)

---

## 1. 这个项目是做什么的

`eagle-go` 是一个 Go 语言写的微服务"底座"——不是一个完整的业务系统，
而是一套可以在上面继续搭业务的基础设施：谁登录了系统（认证）、
这个人能不能调这个接口（授权）、以及一个通用的"字典"模块（下拉框选项这类
配置数据）。

目前仓库里只有一个可部署进程，入口是 `cmd/server`。权限、字典、文件、
通知是同一进程里的业务模块，位于 `internal/modules/`，不是四个微服务。

一句话概括技术栈：**Go + Kratos 框架 + PostgreSQL 存数据
+ Keycloak 管用户 + Casbin 判权限**。下一节会逐个简单解释这些是什么。

## 2. 开始之前：几个会反复出现的名词

不需要精通，只需要知道"这是干嘛的"，遇到时不会一头雾水：

| 名词 | 是什么 | 在这个项目里的角色 |
|---|---|---|
| **Go module** | Go 的依赖管理机制，`go.mod`/`go.sum` 就是它的产物 | 本仓库所有第三方库的版本都锁在这里 |
| **Protobuf / buf** | 一种"接口描述语言"，先写 `.proto` 文件定义接口长什么样，再用工具生成代码 | 本项目所有对外接口（`api/` 目录）和内部配置结构（`config.proto`）都先写 proto，再生成 Go 代码。`buf` 是管理 proto 文件的工具链 |
| **gRPC / HTTP** | 两种网络协议。本项目的每个接口同时支持这两种——写一份 proto，两种协议的代码都生成好了 | 你用 `curl` 调的是 HTTP；服务之间互相调用更常用 gRPC |
| **ent** | 用 Go 代码描述数据库表结构（见 `ent/schema/`），再生成类型安全的查询代码 | `ent/` 目录下 90% 以上的代码都是自动生成的，你不需要看懂，只需要知道改 `ent/schema/*.go` 然后重新生成即可 |
| **Kratos** | 一个 Go 服务框架（B站开源），负责 HTTP/gRPC 怎么启动、中间件怎么串、日志和链路怎么接 | 当前进程入口在 `cmd/server` |
| **Keycloak** | 一个开源的"账号中心"（IAM），管理用户名、密码、角色 | 本项目**不**自己存用户密码，登录、发 token 这些事全部交给 Keycloak |
| **OIDC / JWT / token** | OIDC 是基于 OAuth2 的登录协议；登录成功后拿到一个 JWT（一段自包含、带签名的字符串），后续请求带着它证明"我是谁" | 你调接口时要在请求头里带 `Authorization: Bearer <token>` |
| **Casbin** | 一个权限判定引擎：给定"谁"和"要做什么"，回答"允许还是拒绝" | Keycloak 回答"你是谁、有什么角色"，Casbin 回答"这个角色能不能调这个接口" |
| **DDD / Clean Architecture** | 一种代码组织方式，核心思想是按业务模块隔离，并让业务规则不依赖数据库和框架 | 每个模块内部使用 `domain`/`application`/`infrastructure`/`interfaces` 四层，第 6 节会展开讲 |

## 3. 环境准备

必须要装的：

- **Go**：`go.mod` 里锁定的是 `go 1.26.5`。如果你本机版本更低，
  较新的 Go 会在编译时自动下载匹配的工具链（需要联网），不用你手动升级。
  用 `go version` 确认当前版本。
- **Docker Desktop**（或者其他能跑 `docker compose` 的环境）：
  PostgreSQL、Keycloak 都用它启动，不需要在本机分别安装。
- **Git**，以及能跑 bash 脚本的终端。本项目的 `Makefile` 和文档里的命令
  都按 bash 语法写的；Windows 用户建议装 **Git Bash**（装 Git 时自带）
  或者用 WSL，不建议直接用 PowerShell/cmd 跟着抄命令。

推荐但非必须：

- 一个懂 Go 的编辑器：VS Code（装 Go 插件）或 GoLand，能省掉很多
  "这个类型从哪来的"的排查时间。
- 一个能发 HTTP 请求的工具：`curl` 就够用（本文档全用它），
  或者 Postman / Apifox 之类的图形化工具。

## 4. 手把手跑起来

按顺序做，每一步都有对应的验证方法，出问题时能立刻定位是哪一步错了。

### 第 1 步：拿到代码

```bash
git clone <你的仓库地址>
cd eagle-go
```

### 第 2 步：装开发期工具链

这一步装的是"生成代码"要用到的命令行工具，不是项目依赖本身（项目依赖
由 `go build`/`go test` 自动处理）。

```bash
make init
```

它会装 `buf`（proto 代码生成）、`goose`（数据库迁移工具）、
`golangci-lint`（静态检查）。protobuf 插件由 `go.mod` 锁定并通过
`go tool` 自动运行。装完之后，
确认它们都能被找到：

```bash
buf --version && goose --version && echo "工具链就绪"
```

如果提示命令找不到，通常是 `$GOPATH/bin`（一般是 `~/go/bin`）没加进
`$PATH`，把它加进你的 shell 配置文件（`~/.bashrc` 等）里再重新打开终端。

### 第 3 步：生成代码

这是新手最容易懵的一步——"为什么还要生成代码，代码不是已经在仓库里了吗"。

答案是：**大部分生成结果确实已经提交在仓库里了**（比如 `api/*/v1/*.pb.go`、
`ent/` 下的绝大多数文件），你其实不跑这一步也能直接
`go build` 成功。只有当你**修改了** `.proto` 文件或者 `ent/schema/`
下的表结构定义时，才需要重新跑生成，让生成结果和你的修改保持同步。

第一次跑起来项目，建议还是完整跑一遍，确认工具链没问题：

```bash
make generate # 生成 API、内部配置与 Ent 数据访问代码
```

跑完用 `git status` 看一下——正常情况下应该**没有任何文件变化**
（因为生成结果本来就和仓库里的一致）。如果有变化，说明生成结果和
仓库里的不一致，一般是工具版本不对；生成插件和 Ent 版本都由
`go.mod` 固定，不要改用全局安装的插件绕过它。

### 第 4 步：启动依赖服务

用 Docker 把 PostgreSQL、Keycloak 拉起来：

```bash
docker compose -f deploy/docker-compose.yml up -d postgres keycloak
```

第一次拉镜像会花几分钟。用下面的命令确认三个容器都健康：

```bash
docker compose -f deploy/docker-compose.yml ps
```

`STATUS` 列应该都是 `Up` / `healthy`。Keycloak 启动稍慢（要初始化数据库、
导入 realm 配置），耐心等 30 秒到 1 分钟，可以用下面的命令看它是否就绪：

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/realms/eagle
```

返回 `200` 就说明 Keycloak 已经把 `deploy/keycloak/realm-eagle.json`
这个 realm 配置导入好了（里面预置了 `admin`/`user` 两个角色和三个客户端，
但**没有预置任何用户**——这是刻意的，见第 8 步）。

### 第 5 步：执行数据库迁移

上一步启动的 PostgreSQL 是空的，还没有表。用 `goose` 跑迁移脚本
（脚本在 `db/migrations/` 下，是纯 SQL 文件，可以直接打开看）：

```bash
goose -dir db/migrations postgres "postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable" up
```

看到几行 `OK` 就说明迁移成功了。这一步会建好权限树、字典、Casbin
策略表，并且种入一些初始数据（比如种子权限节点）。

### 第 6 步：启动后端服务

```bash
go run ./cmd/server -conf configs
```

正常会看到几行 JSON 格式的启动日志，进程会一直挂在前台（这是正常的，
这是个长期运行的服务，不是跑一次就退出的脚本）。开一个新终端标签页
继续后面的步骤。

`-conf configs` 指向配置文件目录，实际读的是
`configs/config.yaml`。这个文件里的地址（`127.0.0.1:5432`、
`127.0.0.1:8080`）都对应第 4 步用 Docker 起的服务，
不需要改就能直接用。

### 第 7 步：确认服务活着

不用马上就去啃 Keycloak 认证那一套，先用一个不需要登录的端点确认
服务本身没问题：

```bash
curl http://127.0.0.1:9100/healthz
```

返回 `ok` 就说明 HTTP/gRPC 服务器和数据库连接都已经成功
建立起来了（这几步任何一个失败，服务在启动阶段就会直接崩溃退出，
不会跑到能响应请求的状态）。

### 第 8 步：在 Keycloak 里创建一个用户

前面说过 realm 里刻意没有预置用户（避免"内置账号+已知密码"这种东西
一路带到生产环境的安全隐患）。用 Keycloak 的管理员账号
（`admin`/`admin`，在 docker-compose 里配的）创建一个：

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh config credentials \
  --server http://localhost:8080 --realm master --user admin --password admin
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh create users -r eagle \
  -s username=alice -s enabled=true \
  -s firstName=Alice -s lastName=Test -s email=alice@example.com
```

> `firstName`/`lastName` 不能省。Keycloak 26 有个叫 `VERIFY_PROFILE` 的
> 必需动作，缺了这两个字段会导致登录报 `Account is not fully set up`，
> 而这个报错信息完全看不出问题出在用户资料不全。

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh set-password -r eagle \
  --username alice --new-password 'Passw0rd!'
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh add-roles -r eagle \
  --uusername alice --rolename admin
```

给 `alice` 授予的是 realm `admin` 角色。它不会触发 client role 的
超管短路，但数据库种子已经给对应策略键 `realm:admin` 授予 `system:*`，
所以适合用来做第一次尝试，不用先去后台配权限策略。

### 第 9 步：换取 token，调用第一个接口

先用刚创建的用户名密码换一个 access token：

```bash
curl -s -d client_id=eagle-web -d username=alice -d 'password=Passw0rd!' \
  -d grant_type=password \
  http://127.0.0.1:8080/realms/eagle/protocol/openid-connect/token
```

返回的 JSON 里 `access_token` 字段就是你要的 token（一长串以 `eyJ`
开头的字符串）。把它存到变量里方便后面用：

```bash
TOKEN=$(curl -s -d client_id=eagle-web -d username=alice -d 'password=Passw0rd!' \
  -d grant_type=password \
  http://127.0.0.1:8080/realms/eagle/protocol/openid-connect/token | \
  grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
```

用它调一个真实业务接口：

```bash
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/v1/system/permissions
```

如果看到一段 JSON（权限树的列表），恭喜，从"启动依赖"到"认证鉴权"
的完整链路已经跑通了。不带 `Authorization` 头再试一次，应该拿到 401：

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8000/v1/system/permissions
```

## 5. 跑测试

```bash
go test ./...
```

这条命令**不需要 Docker**——集成测试用的是
[embedded-postgres](https://github.com/fergusstrange/embedded-postgres)，
会在进程内自己拉起一个真实的 PostgreSQL（第一次跑要下载约 100MB 的
数据库二进制，之后会缓存在 `~/.embedded-postgres-go/`，几秒就能起来）。

只想跑快的单元测试、跳过需要拉数据库的集成测试：

```bash
go test -short ./...
```

测试分了几个层次，越往下跑得越快、越不依赖外部环境：

| 位置 | 测什么 | 要不要外部依赖 |
|---|---|---|
| `internal/modules/access/domain/` | 纯业务规则（权限码格式、防环逻辑……），零外部依赖 | 不要，0.4 秒跑完 |
| `pkg/authz/` | Casbin 判定逻辑、鉴权中间件 | 不要（内存 Casbin） |
| `internal/modules/*/infrastructure/` | 数据库和文件存储的真实读写 | 要（embedded-postgres / 临时目录） |
| `tests/e2e/` | 真实 HTTP 服务器 + 真实签名的 JWT，端到端验证 401/403/200 | 要 |

Windows 上如果想跑 `go test -race`（CI 就是这么跑的），需要 cgo，
也就是需要装 C 编译器（比如 MinGW）。不想在本机装的话，可以用容器跑：

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.26 sh -c \
  'useradd -m -u 1500 t && su t -c "cd /src && HOME=/home/t GOPATH=/home/t/go GOCACHE=/home/t/c go test -race ./..."'
```

## 6. 项目目录导览

```
eagle-go/
├── api/eagle/              # 对外接口的 proto 定义（"契约"），改这里要跑 buf generate
│   ├── annotations/v1/     #   自定义的权限注解（给接口标注需要什么权限码）
│   ├── access/v1/          #   权限与角色绑定契约
│   ├── dictionary/v1/      #   字典契约
│   ├── file/v1/            #   文件契约
│   └── notification/v1/    #   站内通知契约
├── cmd/server/             # 程序入口 main.go，以及显式依赖装配代码
├── configs/                # 本地开发配置
├── internal/
│   ├── modules/            #   access、dictionary、file、notification
│   └── platform/           #   config、database、server
├── tests/                  # architecture、e2e、testkit
├── ent/
│   ├── schema/              #   数据库表结构定义（手写），改这里要跑 go generate ./ent
│   └── ...                  #   其余都是生成代码，不需要手改
├── pkg/                     # 可被未来多个进程复用的技术原语
│   ├── authn/                #   验证 Keycloak 签发的 token
│   ├── authz/                #   Casbin 判定器 + 鉴权中间件
│   ├── identity/               #   "当前登录者是谁"这个信息在 context 里怎么传
│   └── db/                    #   PostgreSQL database/sql 连接池
├── db/migrations/            # 数据库迁移脚本（goose 管理，纯 SQL，可以直接读）
├── docs/                      # 给人看的说明：怎么加接口、分层为什么这样
├── .agents/rules/             # 给 AI 的编码约束（人一般不用看）
└── deploy/                    # Docker Compose、Keycloak realm 配置、可观测性栈配置
```

拿到一个具体任务时，可以按"这个改动的性质"去定位：

- **要改一个接口的入参/返回字段** → 从 `api/eagle/*/v1/*.proto` 开始
- **要改一条业务规则**（比如"权限码格式要求"）→ 对应模块的 `internal/modules/<module>/domain/`
- **要改一个数据库表的结构** → `ent/schema/`，然后在 `db/migrations/`
  里手写一条新的迁移 SQL（ent 生成的建表能力在本项目**没有**被使用，
  见 README 的说明——迁移统一走 goose）
- **要改 SQL 查询写法** → 对应模块的 `internal/modules/<module>/infrastructure/`
- **要改中间件链、启动流程** → `internal/platform/server/`、`cmd/server/main.go`

## 7. 接下来：怎么用这个底座

服务能调通之后，去 [docs/usage.md](docs/usage.md)。那里用刚转 Go 的人能看懂的话，
按任务写了：

- 加一个需要权限的接口（改 proto → 写入权限目录 → 生成代码 → 补分层）
- 给角色授权、查自己的菜单和权限码
- 业务代码里怎么取出当前登录人
- 加一整块 CRUD 要改哪些文件、什么时候需要改显式装配
- 从别的语言转过来最容易做错的几件事

权限码怎么进目录、为什么不能在建菜单时发明新码，也在那份文档里。

## 8. 常见问题 / 踩坑 FAQ

**Q: 改了配置文件，服务启动报 `invalid google.protobuf.Duration value`，
但看不出是哪个字段错了。**

A: 所有时长字段（`timeout`、`*_ttl` 这类）只认「数字 + s」的写法，
比如 `3600s`、`0.5s`。写成 Go 那种 `1h`/`30m`/`500ms` 会解析失败。
把配置文件里所有时长字段过一遍格式即可。

**Q: 创建 Keycloak 用户后登录报 `Account is not fully set up`。**

A: 创建用户时忘了带 `-s firstName=xxx -s lastName=xxx`。Keycloak 26
有个"资料补全"的强制动作，缺这两个字段就会卡住。回到第 8 步补上。

**Q: 我改了 `.proto` 或 `ent/schema/`，忘了重新生成代码就直接跑
`go build`，结果编译不过或者行为对不上。**

A: 生成代码不会自动跟着源文件变化，改完一定要手动跑对应的生成命令
（见第 3 步），或者直接跑 `make generate`。CI 里有一步专门检查
"生成代码是不是和 proto 一致"，本地忘了跑的话 PR 会在这一步被拦下来。

**Q: `go test -race` 在 Windows 上报 `-race requires cgo`。**

A: `-race` 需要 cgo，也就是需要 C 编译器。本机没装 MinGW 的话，
按第 5 节末尾给的容器命令跑，或者干脆跑不带 `-race` 的
`go test ./...`（日常开发够用，CI 会用 Linux 跑一遍 `-race` 兜底）。

**Q: 调接口一直 401，我确定 token 是对的。**

A: 先看 `configs/config.yaml` 里 `auth.issuer` 的值是否和
Keycloak 实际的 issuer 完全一致（协议、端口、有没有尾部斜杠都要对上）。
容器化部署时还要注意 `jwks_url`：token 里公开的 issuer 地址
（外网能访问的域名）和资源服务器实际该访问的地址（集群内地址）
经常不是同一个，这时要显式配置 `jwks_url`。

**Q: 调接口一直 403，但我确定这个角色应该有权限。**

A: 常见原因有两个：一是这个角色确实没在 Casbin 里配对应的权限码
（用 `RoleBindingService` 相关接口查一下）；二是权限码本身写错了
（比如三段式的 `system:user:add` 少写了一段）。`internal/modules/access/domain/`
下有针对权限码格式和通配符边界的单元测试，读一下能快速建立起
"什么样的权限码是合法的"这个概念。

**Q: 加一个新接口需要什么权限码，要改哪里？**

A: 完整步骤见 [docs/usage.md](docs/usage.md) 第 4 节。短答案：
1. 在 proto 方法上声明 `access` 和 `(eagle.annotations.v1.perm) = "system:foo:add"`；
2. 把同一条码写入 `permission_definition`（改种子或加一条 goose 迁移）；
3. 启动时会校验 proto 与目录一致，对不上服务起不来；
4. 需要菜单/按钮时，再在导航树上引用这条已有的码。

不要在管理端「先建一个按钮再长出权限码」。

## 9. 想深入了解，看这里

- [docs/usage.md](docs/usage.md)：底座怎么用——加接口、配权限、拿当前用户
- [docs/architecture.md](docs/architecture.md)：分层和授权边界
- [AGENTS.md](AGENTS.md) / [`.agents/rules/`](.agents/rules/)：给 AI 的编码约束
- [README.md](README.md)：这个项目"为什么这么设计"——分层架构的取舍、
  权限模型的设计、Kratos v3 相对 v2 的坑、服务间认证怎么做等等
- [Kratos 官方文档](https://go-kratos.dev/)：框架本身怎么用
- [ent 官方文档](https://entgo.io/)：Schema as Code 的 ORM，怎么定义
  表结构、怎么写查询
- [buf 官方文档](https://buf.build/docs)：proto 文件怎么组织、
  `buf generate`/`buf lint`/`buf breaking` 分别做什么
- [Casbin 官方文档](https://casbin.org/)：权限模型（RBAC/ABAC）、
  策略语法
- [Keycloak 官方文档](https://www.keycloak.org/documentation)：
  Realm、Client、Role 这些概念的详细说明
