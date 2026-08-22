# 模块与分层

先按业务模块组织，再在模块内使用 DDD 四层：

`service → application → domain ← infrastructure`

| 改什么 | 落在哪 |
|---|---|
| 不变量、值对象、仓储/存储端口、领域错误 | `app/<service>/internal/<module>/domain` |
| 跨聚合或跨端口用例编排 | `app/<service>/internal/<module>/application` |
| SQL / Ent / 事务 / Casbin / 文件存储适配 | `app/<service>/internal/<module>/infrastructure` |
| proto ↔ domain、当前主体传递 | `app/<service>/internal/<module>/service` |
| 中间件链、HTTP/gRPC 注册、统一错误映射 | `pkg/platform/server` |
| 数据库连接生命周期与服务 Ent Client | `app/<service>/internal/platform/database` |
| 跨服务 API 客户端适配器 | 调用方模块 `infrastructure` |
| 验签、鉴权中间件、健康检查、身份 context | `pkg/` |

- 新业务先建立 `app/<service>/internal/<module>`，再在模块内分层；不要建立服务级全局 `domain/data/service`。
- 新服务入口放 `app/<service>/cmd/<service>`，它只能组合该服务拥有的模块；禁止 import 其他服务模块。
- 跨服务调用依赖对方 `api/` 契约并在 infrastructure 实现本模块端口，application/domain 不碰 protobuf client。
- 有不变量才写聚合根。字典、通知等简单模型保持贫血，不为凑 DDD 术语加领域事件或工厂。
- 需要锁才能判断的树/并发规则只在 infrastructure 事务里做，application 不预检（避免 TOCTOU）。
- 仓储接口每个方法必须有生产调用方。
- infrastructure 重建私有字段实体时用 Snapshot + Rehydrate，不用会校验新规则的构造器。
- `domain` 只依赖标准库；`application` 只依赖 domain 与标准库。
- 只有模块 infrastructure 与服务内 `internal/platform/database` 可以 import 本服务 Ent；`pkg/` 禁止 import `app/`。

禁止：把四层收成 handler + repo；也禁止让简单 CRUD 模块复制 access 模块的聚合根仪式。
