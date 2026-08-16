# 契约、权限、身份

加后端接口的顺序见 [usage.md](usage.md) 第 4 节，不可颠倒：proto（`access` + 需要时 `perm`）→ 新码写入 `permission_definition` → 生成 → domain/data/biz/service → 新错误挂 `ErrorMapping` → 需要时 `make wire`。

- 每个 RPC 必须有 `access`。handler 里禁止写鉴权 if。
- 权限码：具体码严格三段 `domain:resource:action`；通配只能末段 `*`。禁止 `system:*:add`、`sys*:x:y`、Casbin `keyMatch2`（会把 `:xxx` 当路径参数，只读变全体写）。
- 导航节点只能引用目录里已启用的具体码。创建菜单不能发明新契约。
- 角色在 Keycloak。本库不建用户表、不存「这个人是什么角色」，只存角色→权限和角色继承。
- 当前用户：`pkg/identity.FromContext`。不要自己解析 JWT，不要第二套 Principal。
- 策略写入必须走 `Replace*IfVersion` / `Add*IfVersion`（同事务 version++ 与审计）。禁止 `SavePolicy` / `AddPolicy`。
- 超管只认本服务 client role，不认 realm 同名角色。
- 校验写在 proto（protovalidate），service 不要再写一遍同样的必填/长度检查。
- 错误原因加在 `error_reason.proto`，再挂 `ErrorMapping`。不要引入 `protoc-gen-go-errors`。
- 破坏性契约变更会被 `buf breaking` 拦住；改语义就新增字段，不要复用旧号。
