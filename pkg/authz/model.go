package authz

// ModelText 是 Casbin 的 RBAC 模型定义。
//
// 请求与策略都是 (角色, 权限码) 二元组：
//
//	r = sub(角色), obj(权限码)
//	p = sub(角色), obj(权限码)
//
// 用户与角色的归属由 auth 模块维护，角色随 Eagle token 下发。
// 中间件遍历已验证主体的角色逐个判定；Casbin 的 p 保存角色授权，g 仅保存角色继承，
// 不重复存储账号与角色的绑定。
//
// 通配用 keyMatch 而不是 keyMatch2。
//
// keyMatch2 是为 URL 路径设计的，会把 `:xxx` 解析成路径参数占位符。
// 我们的权限码正是冒号分隔的，于是 `system:permission:query` 会被当成
// 模式 `system:{任意}:{任意}`，与 `system:permission:add` 匹配成功——
// 结果是任何同形状的权限码互相等价，只读角色直接获得写权限。
//
// keyMatch 只认 `*`：`system:*` 覆盖整个 system 域，
// 不含 `*` 的策略退化为精确比较，符合权限码的语义。
const ModelText = `
[request_definition]
r = sub, obj

[policy_definition]
p = sub, obj

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (r.obj == p.obj || keyMatch(r.obj, p.obj))
`
