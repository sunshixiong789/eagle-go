package domain

import (
	"context"
	"fmt"
	"time"
)

// PermissionType 是权限节点类型值对象。
type PermissionType int32

// 权限节点类型取值。
const (
	PermissionTypeDir    PermissionType = 1 // 目录
	PermissionTypeMenu   PermissionType = 2 // 菜单
	PermissionTypeButton PermissionType = 3 // 按钮
)

// Valid 报告类型取值是否在允许范围内。
func (t PermissionType) Valid() bool {
	return t >= PermissionTypeDir && t <= PermissionTypeButton
}

// IsButton 表示这是按钮级权限，不进菜单树。
func (t PermissionType) IsButton() bool { return t == PermissionTypeButton }

// Status 是启用状态值对象。
type Status int32

// 状态取值。
const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

// Enabled 报告是否处于启用状态。
func (s Status) Enabled() bool { return s == StatusEnabled }

// RootPermissionID 是顶级节点的 parent 取值。
// 用 0 而非 NULL：树的根是领域概念，不该由数据库的可空性来表达。
const RootPermissionID int64 = 0

// Permission 是权限树的聚合根。
//
// 不变量由实体自己守护而不是散在用例里：
//   - 名称非空
//   - 类型取值合法
//   - 按钮必须携带权限码
//   - 父节点不能是自身或自身的后代
//
// 这些规则在 Rename / Recode / Reparent 等方法里就地校验，
// 因此不存在「构造出一个违反不变量的 Permission」的路径。
type Permission struct {
	id        int64
	parentID  int64
	name      string
	code      PermissionCode
	typ       PermissionType
	path      string
	component string
	icon      string
	sort      int32
	visible   bool
	status    Status
	createdAt time.Time
	updatedAt time.Time
	revision  int64
}

// NewPermissionParams 是创建权限节点的入参。
type NewPermissionParams struct {
	ParentID  int64
	Name      string
	Code      string
	Type      int32
	Path      string
	Component string
	Icon      string
	Sort      int32
	Visible   bool
	Status    int32
}

// NewPermission 创建权限节点，构造即校验全部不变量。
func NewPermission(p NewPermissionParams) (*Permission, error) {
	code, err := NewPermissionCode(p.Code)
	if err != nil {
		return nil, err
	}

	perm := &Permission{
		parentID:  p.ParentID,
		name:      p.Name,
		code:      code,
		typ:       PermissionType(p.Type),
		path:      p.Path,
		component: p.Component,
		icon:      p.Icon,
		sort:      p.Sort,
		visible:   p.Visible,
		status:    Status(p.Status),
	}
	if err := perm.validate(); err != nil {
		return nil, err
	}
	return perm, nil
}

// RehydratePermission 从持久化状态重建实体，仅供 data 层调用。
//
// 刻意不走 NewPermission 的校验：库里的历史数据可能是在规则收紧之前
// 写入的，重建时报错会让整张表读不出来。校验只在写入路径上执行。
func RehydratePermission(
	id, parentID int64, name, code string, typ, status int32,
	path, component, icon string, sort int32, visible bool,
	createdAt, updatedAt time.Time, revision int64,
) *Permission {
	return &Permission{
		id:        id,
		parentID:  parentID,
		name:      name,
		code:      PermissionCode{value: code},
		typ:       PermissionType(typ),
		path:      path,
		component: component,
		icon:      icon,
		sort:      sort,
		visible:   visible,
		status:    Status(status),
		createdAt: createdAt,
		updatedAt: updatedAt,
		revision:  revision,
	}
}

func (p *Permission) validate() error {
	if p.name == "" {
		return ErrEmptyPermissionName
	}
	if !p.typ.Valid() {
		return fmt.Errorf("%w: %d", ErrInvalidPermissionType, p.typ)
	}
	// 导航节点引用的是一个具体后端能力；通配符只允许出现在角色策略中。
	// 把 system:* 之类的覆盖性策略登记成一个按钮，会让目录与授权边界混淆。
	if p.code.HasWildcard() {
		return fmt.Errorf("%w: 导航节点只能引用具体权限码", ErrInvalidPermissionCode)
	}
	// 按钮不挂权限码就永远无法被授权，等于一个点不动的死按钮。
	// 这类配置错误在运行时表现为「有按钮但一直 403」，很难查。
	if p.typ.IsButton() && p.code.IsZero() {
		return ErrButtonRequiresCode
	}
	return nil
}

// ── 访问器 ────────────────────────────────────────────────

// ID 返回节点标识。
func (p *Permission) ID() int64 { return p.id }

// ParentID 返回父节点标识，顶级节点为 RootPermissionID。
func (p *Permission) ParentID() int64 { return p.parentID }

// Name 返回节点名称。
func (p *Permission) Name() string { return p.name }

// Code 返回权限码。
func (p *Permission) Code() PermissionCode { return p.code }

// Type 返回节点类型。
func (p *Permission) Type() PermissionType { return p.typ }

// Path 返回前端路由路径。
func (p *Permission) Path() string { return p.path }

// Component 返回前端组件路径。
func (p *Permission) Component() string { return p.component }

// Icon 返回图标标识。
func (p *Permission) Icon() string { return p.icon }

// Sort 返回排序值。
func (p *Permission) Sort() int32 { return p.sort }

// Visible 返回菜单是否可见。
func (p *Permission) Visible() bool { return p.visible }

// Status 返回启用状态。
func (p *Permission) Status() Status { return p.status }

// CreatedAt 返回创建时间。
func (p *Permission) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt 返回更新时间。
func (p *Permission) UpdatedAt() time.Time { return p.updatedAt }

// Revision 返回读取该节点时整棵权限树的版本。
func (p *Permission) Revision() int64 { return p.revision }

// IsRoot 表示这是顶级节点。
func (p *Permission) IsRoot() bool { return p.parentID == RootPermissionID }

// IsMenuNode 表示该节点应出现在菜单树中（目录或菜单，不含按钮）。
func (p *Permission) IsMenuNode() bool { return !p.typ.IsButton() }

// ── 行为 ──────────────────────────────────────────────────

// Update 整体更新可变字段，任一不变量被破坏则整体不生效。
func (p *Permission) Update(params NewPermissionParams) error {
	code, err := NewPermissionCode(params.Code)
	if err != nil {
		return err
	}

	// 先在副本上应用变更，校验通过后才写回本体，
	// 避免校验失败时留下一个被改坏一半的实体
	next := *p
	next.parentID = params.ParentID
	next.name = params.Name
	next.code = code
	next.typ = PermissionType(params.Type)
	next.path = params.Path
	next.component = params.Component
	next.icon = params.Icon
	next.sort = params.Sort
	next.visible = params.Visible
	next.status = Status(params.Status)

	if err := next.validate(); err != nil {
		return err
	}

	*p = next
	return nil
}

// EnsureDeletable 校验删除前置条件。childCount 由调用方查得。
func (p *Permission) EnsureDeletable(childCount int64) error {
	if childCount > 0 {
		return ErrPermissionHasChildren
	}
	return nil
}

// PermissionTree 是权限节点的集合视图，承载需要纵观全树才能判断的规则。
//
// 单个聚合根看不到兄弟和祖先，因此「防环」「补全祖先链」这类
// 跨节点的规则放在这里，而不是硬塞进 Permission。
type PermissionTree struct {
	byID map[int64]*Permission
	all  []*Permission
}

// NewPermissionTree 用平铺的节点列表构建树视图。
func NewPermissionTree(perms []*Permission) *PermissionTree {
	byID := make(map[int64]*Permission, len(perms))
	for _, p := range perms {
		byID[p.id] = p
	}
	return &PermissionTree{byID: byID, all: perms}
}

// All 返回全部节点，顺序与构建时一致。
func (t *PermissionTree) All() []*Permission { return t.all }

// Get 按 ID 取节点。
func (t *PermissionTree) Get(id int64) (*Permission, bool) {
	p, ok := t.byID[id]
	return p, ok
}

// EnsureNoCycle 校验把 id 挂到 newParentID 之下不会形成环。
//
// 上溯 newParentID 的祖先链，遇到 id 即说明 newParentID 是 id 的后代。
// 加深度上限是为了在数据本身已成环时也能终止，而不是死循环。
func (t *PermissionTree) EnsureNoCycle(id, newParentID int64) error {
	if newParentID == RootPermissionID {
		return nil
	}
	if newParentID == id {
		return ErrPermissionCycle
	}

	const maxDepth = 64
	cursor := newParentID
	for i := 0; i < maxDepth; i++ {
		node, ok := t.byID[cursor]
		if !ok {
			return ErrPermissionNotFound
		}
		if node.parentID == RootPermissionID {
			return nil
		}
		if node.parentID == id {
			return ErrPermissionCycle
		}
		cursor = node.parentID
	}
	return ErrPermissionCycle
}

// VisibleMenus 返回持有 granted 这批权限码的主体可见的菜单节点。
//
// 除了直接命中的节点，还会补全它们的祖先链：
// 少了父目录，子菜单在前端就挂不上树，表现为「有权限却看不到入口」。
func (t *PermissionTree) VisibleMenus(granted []PermissionCode) []*Permission {
	visible := make(map[int64]struct{}, len(t.all))
	for _, p := range t.all {
		if p.code.IsZero() {
			// 目录自身没有权限码，靠子节点带出来
			continue
		}
		if !p.status.Enabled() {
			continue
		}
		if grantedCovers(granted, p.code) {
			visible[p.id] = struct{}{}
		}
	}

	for id := range visible {
		for cur, ok := t.byID[id]; ok && !cur.IsRoot(); {
			parent, found := t.byID[cur.parentID]
			if !found {
				break
			}
			if _, seen := visible[parent.id]; seen {
				break
			}
			visible[parent.id] = struct{}{}
			cur = parent
		}
	}

	// 按原始顺序输出，保证菜单排序稳定
	out := make([]*Permission, 0, len(visible))
	for _, p := range t.all {
		if _, ok := visible[p.id]; !ok {
			continue
		}
		if p.IsMenuNode() {
			out = append(out, p)
		}
	}
	return out
}

// grantedCovers 判断任一已授予的策略是否覆盖目标权限码。
// 与鉴权判定共用 PermissionCode.Covers，因此 system:* 这类末段通配
// 会让对应菜单可见，而不会只匹配字面量相等。
func grantedCovers(granted []PermissionCode, target PermissionCode) bool {
	for _, g := range granted {
		if g.Covers(target) {
			return true
		}
	}
	return false
}

// KnownCodes 返回树中全部非空权限码，用于校验授权时引用的权限码是否存在。
func (t *PermissionTree) KnownCodes() map[string]struct{} {
	out := make(map[string]struct{}, len(t.all))
	for _, p := range t.all {
		if !p.code.IsZero() {
			out[p.code.String()] = struct{}{}
		}
	}
	return out
}

// ListPermissionsQuery 是权限列表的查询条件。
type ListPermissionsQuery struct {
	Status *Status
	Type   *PermissionType
}

// PermissionRepo 是权限聚合的仓储接口，由基础设施层实现。
type PermissionRepo interface {
	Create(ctx context.Context, p *Permission) (*Permission, error)
	GetByID(ctx context.Context, id int64) (*Permission, error)
	List(ctx context.Context, q ListPermissionsQuery) ([]*Permission, error)
	Update(ctx context.Context, p *Permission, expectedRevision *int64) (*Permission, error)
	Delete(ctx context.Context, id int64, expectedRevision *int64) error
	CountChildren(ctx context.Context, id int64) (int64, error)
	KnownCodes(ctx context.Context) (map[string]struct{}, error)
}
