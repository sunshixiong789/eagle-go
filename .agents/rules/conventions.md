# 规则目录

本目录是规范正文。AI 每次只加载根目录 [AGENTS.md](../../AGENTS.md)，按任务打开下表对应文件，不要通读。

| 主题 | 文件 |
|---|---|
| 加接口、配权限、拿当前用户 | [usage.md](usage.md)（第 4 节加接口，第 7 节整块 CRUD） |
| 分层理由、底座怎么长、何时拆服务 | [architecture.md](architecture.md) |
| 分层落点、规则只在一处 | [layers.md](layers.md) |
| 契约、权限码、身份 | [api-authz.md](api-authz.md) |
| Ent、迁移、Redis、策略写入 | [data.md](data.md) |
| Go 写法、依赖、错误、注释 | [style.md](style.md) |
| 测试分层 | [testing.md](testing.md) |

约定变了：先改代码和（如有）架构测试，再改对应规则和 `AGENTS.md` 索引。
