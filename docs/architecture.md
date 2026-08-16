# Eagle 架构边界

本仓库采用模块化单体作为当前部署形态，并保留按业务域拆分服务的边界。服务内依赖方向固定为：

`transport/service → biz → domain ← data`

- `domain` 只包含领域模型、规则和仓储接口，不依赖 Kratos、protobuf、Ent、Redis 或 Casbin。
- `biz` 负责编排跨聚合用例，不处理 HTTP/gRPC 错误。
- `service` 是传输适配层，统一把领域错误映射为外部错误契约。
- `data` 是唯一允许直接依赖 Ent 的服务内包，负责事务、并发控制、缓存和外部系统适配。
- `pkg` 是跨服务技术能力，禁止反向依赖任何 `app` 包。

这些规则由 `app/system/internal/architecture/dependencies_test.go` 在 CI 中检查。

## 数据与授权边界

- `permission_definition` 是后端 RPC 权限目录；`navigation_node` 是前端导航。隐藏或删除导航不会删除后端授权契约。
- 用户与角色归属由 Keycloak 维护，本库只保存“角色 → 权限”和角色继承，避免双写用户角色数据。
- 超管短路只认 `resource_access.<client_id>.roles` 中的本服务 client role；realm 同名角色不能跨服务扩权。
- 策略写入、版本递增、审计和 Outbox 在同一数据库事务提交；各副本经 Redis 通知并由版本对账兜底。
- 权限树和策略写入分别使用单调 revision/version 做乐观并发控制。

## 服务拆分规则

新增服务时必须拥有自己的 `app/<service>/internal/{domain,biz,data,service}`，只通过 API 契约或事件交换数据。禁止跨服务直接导入对方的 `internal` 包，也禁止直接读写对方拥有的表。迁移仍可由同一流水线执行，但每张表的所有权必须在所属服务的架构文档中唯一声明。
