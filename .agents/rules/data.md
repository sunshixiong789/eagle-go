# 数据、Ent、迁移

- 改表：`app/<service>/internal/platform/database/ent/schema` → 手写 `app/<service>/migrations/NNNNN_*.sql` → `make ent`。生产只认 goose，不用 ent 自动迁移。
- 每个服务使用独立 database 和迁移目录。禁止跨服务查询、外键、事务或把别人的表复制进自己的迁移。
- 迁移必须可回滚（CI 会 `up → down-to 0 → up`）。`down` 不能是空操作，除非变更不可逆并在 SQL 注释写明。
- 权限树 revision、策略 version：冲突返回 `domain.ErrConcurrentModification`。
- 模块 infrastructure 把 Ent/SQL 错误译成领域错误（`NotFound` / 唯一约束）。不要把 `ent.NotFound` 漏到 service。
- 根节点 parent 用 `0` 而不是 SQL NULL。
- 手改服务内 `ent/schema`、`migrations`。禁止手改服务 `ent/` 下生成文件。
- 不要为策略同步加 outbox / 消息队列；现有路径是 version++ 与数据库周期对账。
