# 新手上手指南

这份文档假设你是第一次接触这个项目，甚至第一次接触 Go 微服务开发。
目标是让你能把服务跑起来、成功调通一个接口、看懂目录结构、并且知道
"如果我要改点东西，大概要动哪几个文件"。

如果你已经很熟悉 Go / Kratos / DDD，直接看 [README.md](README.md) 即可——
那里讲的是"为什么这么设计"，信息密度更高，但默认你已经懂这些名词。
这份文档反过来，专注"怎么一步步跑起来"，尽量少默认你已经知道什么。

---

## 目录

1. [这个项目是做什么的](#1-这个项目是做什么的)
2. [开始之前：几个会反复出现的名词](#2-开始之前几个会反复出现的名词)
3. [环境准备](#3-环境准备)
4. [手把手跑起来](#4-手把手跑起来)
5. [跑测试](#5-跑测试)
6. [项目目录导览](#6-项目目录导览)
7. [我想加一个新功能，大概要改哪些地方](#7-我想加一个新功能大概要改哪些地方)
8. [常见问题 / 踩坑 FAQ](#8-常见问题--踩坑-faq)
9. [想深入了解，看这里](#9-想深入了解看这里)

---

## 1. 这个项目是做什么的

`eagle-go` 是一个 Go 语言写的微服务"底座"——不是一个完整的业务系统，
而是一套可以在上面继续搭业务的基础设施：谁登录了系统（认证）、
这个人能不能调这个接口（授权）、以及一个通用的"字典"模块（下拉框选项这类
配置数据）。

目前仓库里只有一个真正跑起来的服务，叫 `system`，位于 `app/system/`。
以后如果要加新的服务（比如订单、库存），会在 `app/` 下再建一个同级目录。

一句话概括技术栈：**Go + Kratos 框架 + PostgreSQL 存数据 + Redis 做缓存
+ Keycloak 管用户 + Casbin 判权限**。下一节会逐个简单解释这些是什么。

## 2. 开始之前：几个会反复出现的名词

不需要精通，只需要知道"这是干嘛的"，遇到时不会一头雾水：

| 名词 | 是什么 | 在这个项目里的角色 |
|---|---|---|
| **Go module** | Go 的依赖管理机制，`go.mod`/`go.sum` 就是它的产物 | 本仓库所有第三方库的版本都锁在这里 |
| **Protobuf / buf** | 一种"接口描述语言"，先写 `.proto` 文件定义接口长什么样，再用工具生成代码 | 本项目所有对外接口（`api/` 目录）和内部配置结构（`conf.proto`）都先写 proto，再生成 Go 代码。`buf` 是管理 proto 文件的工具链 |
| **gRPC / HTTP** | 两种网络协议。本项目的每个接口同时支持这两种——写一份 proto，两种协议的代码都生成好了 | 你用 `curl` 调的是 HTTP；服务之间互相调用更常用 gRPC |
| **ent** | 用 Go 代码描述数据库表结构（见 `ent/schema/`），再生成类型安全的查询代码 | `ent/` 目录下 90% 以上的代码都是自动生成的，你不需要看懂，只需要知道改 `ent/schema/*.go` 然后重新生成即可 |
| **Kratos** | 一个 Go 服务框架（B站开源），负责 HTTP/gRPC 怎么启动、中间件怎么串、日志和链路怎么接 | 本仓库每个服务的入口都在 `app/<name>/cmd/server` |
| **Keycloak** | 一个开源的"账号中心"（IAM），管理用户名、密码、角色 | 本项目**不**自己存用户密码，登录、发 token 这些事全部交给 Keycloak |
| **OIDC / JWT / token** | OIDC 是基于 OAuth2 的登录协议；登录成功后拿到一个 JWT（一段自包含、带签名的字符串），后续请求带着它证明"我是谁" | 你调接口时要在请求头里带 `Authorization: Bearer <token>` |
| **Casbin** | 一个权限判定引擎：给定"谁"和"要做什么"，回答"允许还是拒绝" | Keycloak 回答"你是谁、有什么角色"，Casbin 回答"这个角色能不能调这个接口" |
| **Redis** | 内存数据库，常用作缓存 | 本项目用它做字典数据缓存、以及"token 已登出"的黑名单 |
| **DDD / Clean Architecture** | 一种代码分层方式，核心思想是"业务规则"和"技术细节"（数据库、框架）互相不依赖 | 体现在 `app/system/internal/` 下 `domain`/`biz`/`data`/`service`/`server` 这几层目录，第 6 节会展开讲 |
| **wire** | Google 出的依赖注入代码生成工具 | `app/system/cmd/server/wire_gen.go` 就是它生成的——把整个服务"怎么组装"的代码自动写出来，你不用手写一大坨 `New(...)` 的调用链 |

## 3. 环境准备

必须要装的：

- **Go**：`go.mod` 里锁定的是 `go 1.26.5`。如果你本机版本更低，
  较新的 Go 会在编译时自动下载匹配的工具链（需要联网），不用你手动升级。
  用 `go version` 确认当前版本。
- **Docker Desktop**（或者其他能跑 `docker compose` 的环境）：
  PostgreSQL、Redis、Keycloak 都用它启动，不需要在本机分别装这三个软件。
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

它会装 `wire`（依赖注入代码生成）、`buf`（proto 代码生成）、
`goose`（数据库迁移工具）、`golangci-lint`（静态检查）。装完之后，
确认它们都能被找到：

```bash
wire --help >/dev/null && buf --version && goose --version && echo "工具链就绪"
```

如果提示命令找不到，通常是 `$GOPATH/bin`（一般是 `~/go/bin`）没加进
`$PATH`，把它加进你的 shell 配置文件（`~/.bashrc` 等）里再重新打开终端。

### 第 3 步：生成代码

这是新手最容易懵的一步——"为什么还要生成代码，代码不是已经在仓库里了吗"。

答案是：**大部分生成结果确实已经提交在仓库里了**（比如 `api/*/v1/*.pb.go`、
`ent/` 下的绝大多数文件、`wire_gen.go`），你其实不跑这一步也能直接
`go build` 成功。只有当你**修改了** `.proto` 文件或者 `ent/schema/`
下的表结构定义时，才需要重新跑生成，让生成结果和你的修改保持同步。

第一次跑起来项目，建议还是完整跑一遍，确认工具链没问题：

```bash
buf generate --template buf.gen.yaml        # 生成 api/ 下的接口代码
buf generate --template buf.gen.config.yaml # 生成 app/system/internal/conf 下的配置代码
go generate ./ent                            # 生成 ent/ 下的数据库访问代码
cd app/system/cmd/server && wire && cd -     # 生成依赖注入代码 wire_gen.go
```

跑完用 `git status` 看一下——正常情况下应该**没有任何文件变化**
（因为生成结果本来就和仓库里的一致）。如果有变化，说明生成结果和
仓库里的不一致，一般是工具版本不对，检查 `go.mod` 里 `wire`/`ent`
的版本是否和你本机装的工具版本匹配。

### 第 4 步：启动依赖服务

用 Docker 把 PostgreSQL、Redis、Keycloak 拉起来：

```bash
docker compose -f deploy/docker-compose.yml up -d postgres redis keycloak
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

### 第 6 步：启动 system 服务

```bash
go run ./app/system/cmd/server -conf app/system/configs
```

正常会看到几行 JSON 格式的启动日志，进程会一直挂在前台（这是正常的，
这是个长期运行的服务，不是跑一次就退出的脚本）。开一个新终端标签页
继续后面的步骤。

`-conf app/system/configs` 指向配置文件目录，实际读的是
`app/system/configs/config.yaml`。这个文件里的地址（`127.0.0.1:5432`、
`127.0.0.1:6379`、`127.0.0.1:8080`）都对应第 4 步用 Docker 起的服务，
不需要改就能直接用。

### 第 7 步：确认服务活着

不用马上就去啃 Keycloak 认证那一套，先用一个不需要登录的端点确认
服务本身没问题：

```bash
curl http://127.0.0.1:9100/healthz
```

返回 `ok` 就说明 HTTP/gRPC 服务器、数据库连接、Redis 连接都已经成功
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

给 `alice` 授予的是 `admin` 角色——这个角色在本项目里是"超管"
（见 `app/system/configs/config.yaml` 里的 `super_admin_role`），
会跳过 Casbin 的细粒度判定，用它来做第一次尝试最省事，不用先去
后台配权限策略。

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
数据库二进制，之后会缓存在 `~/.embedded-postgres-go/`，几秒就能起来）；
Redis 那部分用的是纯 Go 实现的 miniredis，同样不需要外部依赖。

只想跑快的单元测试、跳过需要拉数据库的集成测试：

```bash
go test -short ./...
```

测试分了几个层次，越往下跑得越快、越不依赖外部环境：

| 位置 | 测什么 | 要不要外部依赖 |
|---|---|---|
| `app/system/internal/domain/` | 纯业务规则（权限码格式、防环逻辑……），零外部依赖 | 不要，0.4 秒跑完 |
| `pkg/authz/` | Casbin 判定逻辑、鉴权中间件 | 不要（内存 Casbin） |
| `app/system/internal/data/` | 数据库/缓存的真实读写 | 要（embedded-postgres + miniredis，自动拉起） |
| `app/system/internal/e2e/` | 真实 HTTP 服务器 + 真实签名的 JWT，端到端验证 401/403/200 | 要 |

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
│   └── system/v1/          #   system 服务的接口：权限树、字典、角色权限绑定
├── app/system/              # system 服务
│   ├── cmd/server/         #   程序入口 main.go，以及 wire 依赖注入的装配代码
│   ├── configs/             #   本地开发用的配置文件
│   └── internal/
│       ├── domain/         #   业务规则本身：实体、值对象、不变量。不依赖任何数据库/框架
│       ├── biz/             #   用例编排："先查这个、再校验那个、最后落库"，胶水逻辑
│       ├── data/             #   domain 里定义的仓储接口的具体实现（ent 读写数据库、Redis 缓存）
│       ├── service/         #   proto 消息 <-> domain 对象的互相转换
│       ├── server/           #   组装 HTTP/gRPC 服务器、串中间件链
│       ├── conf/             #   配置的 proto 定义和生成代码
│       └── e2e/               #   端到端测试
├── ent/
│   ├── schema/              #   数据库表结构定义（手写），改这里要跑 go generate ./ent
│   └── ...                  #   其余都是生成代码，不需要手改
├── pkg/                       # 多个服务将来会共用的基础设施代码
│   ├── authn/                #   验证 Keycloak 签发的 token
│   ├── authz/                #   Casbin 判定器 + 鉴权中间件
│   ├── identity/               #   "当前登录者是谁"这个信息在 context 里怎么传
│   ├── db/                    #   数据库连接、事务封装
│   └── redisx/                 #   缓存的通用读写逻辑
├── db/migrations/            # 数据库迁移脚本（goose 管理，纯 SQL，可以直接读）
└── deploy/                    # Docker Compose、Keycloak realm 配置、可观测性栈配置
```

拿到一个具体任务时，可以按"这个改动的性质"去定位：

- **要改一个接口的入参/返回字段** → 从 `api/eagle/*/v1/*.proto` 开始
- **要改一条业务规则**（比如"权限码格式要求"）→ `internal/domain/`
- **要改一个数据库表的结构** → `ent/schema/`，然后在 `db/migrations/`
  里手写一条新的迁移 SQL（ent 生成的建表能力在本项目**没有**被使用，
  见 README 的说明——迁移统一走 goose）
- **要改缓存策略、SQL 查询写法** → `internal/data/`
- **要改中间件链、启动流程** → `internal/server/`、`cmd/server/main.go`

## 7. 我想加一个新功能，大概要改哪些地方

拿仓库里已经实现的**字典（Dict）模块**当例子最合适——它是一个纯 CRUD、
没什么复杂业务规则的模块，很适合当"我要加一个类似的新资源"的模板
（如果你的新功能有复杂的业务不变量要守护，再参考"权限树"模块，
即 `internal/domain/permission.go`，那里用了聚合根、值对象这些更"重"
的写法，见 README 的"分层"一节）。

字典模块摸一遍要改的文件，大致是这样的顺序：

1. **`ent/schema/dict.go`**：先定义数据库要存什么字段
2. 跑 `go generate ./ent`，让 ent 根据新的 schema 生成读写代码
3. **`db/migrations/`**：手写一条新的 SQL 迁移文件，真正在数据库里建表/改表
4. **`api/eagle/system/v1/dict.proto`**：定义对外的接口长什么样
   （请求/响应结构、HTTP 路径、需要什么权限码）
5. 跑 `buf generate --template buf.gen.yaml`，生成对应的 Go 代码
6. **`internal/domain/dict.go`**：定义业务对象（`DictType`/`DictData`）
   和仓储接口（`DictRepo`）——这一层不知道数据库、不知道 HTTP，只有"字典
   是什么、能做什么操作"这件事
7. **`internal/data/dict.go`**：实现上一步定义的仓储接口，这里才真正
   出现 ent 的查询代码、Redis 缓存的读写
8. **`internal/biz/dict.go`**：编排用例——比如"更新字典项之后要顺带
   清一次缓存"这种跨步骤的逻辑放在这里
9. **`internal/service/dict.go`**：proto 消息和 domain 对象之间的转换，
   实现 proto 生成的接口
10. **`internal/server/http.go`/`grpc.go`**：把新 service 注册到服务器上
11. **`app/system/cmd/server/wire.go`**（如果新增了 provider 函数）：
    跑 `wire` 重新生成依赖注入代码

看起来步骤很多，但大部分是"新增一个文件、照着同类型的已有文件抄结构"，
真正需要动脑筋的只有第 6 步（业务对象该长什么样）和第 8 步
（有没有跨步骤的编排逻辑）。

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

A: 先看 `app/system/configs/config.yaml` 里 `auth.issuer` 的值是否和
Keycloak 实际的 issuer 完全一致（协议、端口、有没有尾部斜杠都要对上）。
容器化部署时还要注意 `jwks_url`：token 里公开的 issuer 地址
（外网能访问的域名）和资源服务器实际该访问的地址（集群内地址）
经常不是同一个，这时要显式配置 `jwks_url`。

**Q: 调接口一直 403，但我确定这个角色应该有权限。**

A: 常见原因有两个：一是这个角色确实没在 Casbin 里配对应的权限码
（用 `RoleBindingService` 相关接口查一下）；二是权限码本身写错了
（比如三段式的 `system:user:add` 少写了一段）。`internal/domain/`
下有针对权限码格式和通配符边界的单元测试，读一下能快速建立起
"什么样的权限码是合法的"这个概念。

## 9. 想深入了解，看这里

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
