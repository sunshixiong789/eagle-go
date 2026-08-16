# 数据、Ent、迁移

- 改表：`ent/schema` → 手写 `db/migrations/NNNNN_*.sql` → `go generate ./ent`。生产只认 goose，不用 ent 自动迁移。
- 迁移必须可回滚（CI 会 `up → down-to 0 → up`）。`down` 不能是空操作，除非变更不可逆并在 SQL 注释写明。
- 权限树 revision、策略 version：冲突返回 `domain.ErrConcurrentModification`。
- Redis 共用现有连接池（token 撤销、字典缓存、策略 pub/sub），不要再开 Client。
- 缓存编解码用已有 `redisx.JSONCodec[T]`，不要再写一份 codec。
- data 把 Ent/SQL 错误译成领域错误（`NotFound` / 唯一约束）。不要把 `ent.NotFound` 漏到 service。
- 根节点 parent 用 `0` 而不是 SQL NULL。
- 手改 `ent/schema`、`db/migrations`。禁止手改 `ent/` 下生成文件。
- 不要为策略同步加 outbox / 消息队列；现有路径是 version++、Redis 通知、周期对账。
