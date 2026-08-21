# 模块与分层

先按业务模块组织，再在模块内使用 DDD 四层：

`interfaces → application → domain ← infrastructure`

| 改什么 | 落在哪 |
|---|---|
| 不变量、值对象、仓储/存储端口、领域错误 | `internal/modules/<module>/domain` |
| 跨聚合或跨端口用例编排 | `internal/modules/<module>/application` |
| SQL / Ent / 事务 / Casbin / 文件存储适配 | `internal/modules/<module>/infrastructure` |
| proto ↔ domain、当前主体传递 | `internal/modules/<module>/interfaces` |
| 中间件链、HTTP/gRPC 注册、统一错误映射 | `internal/platform/server` |
| 数据库连接生命周期 | `internal/platform/database` |
| 验签、鉴权中间件、健康检查、身份 context | `pkg/` |

- 新业务先建立 `internal/modules/<module>`，不要把代码重新堆回全局 `domain/data/service`。
- 有不变量才写聚合根。字典、通知等简单模型保持贫血，不为凑 DDD 术语加领域事件或工厂。
- 需要锁才能判断的树/并发规则只在 infrastructure 事务里做，application 不预检（避免 TOCTOU）。
- 仓储接口每个方法必须有生产调用方。
- infrastructure 重建私有字段实体时用 Snapshot + Rehydrate，不用会校验新规则的构造器。
- `domain` 只依赖标准库；`application` 只依赖 domain 与标准库。
- 只有模块 infrastructure 与 `internal/platform/database` 可以 import 项目 Ent；`pkg/` 禁止 import `internal/`。

禁止：把四层收成 handler + repo；也禁止让简单 CRUD 模块复制 access 模块的聚合根仪式。
