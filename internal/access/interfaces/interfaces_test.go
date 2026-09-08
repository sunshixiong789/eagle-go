package interfaces

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/internal/access/application"
	"github.com/eagle-go/eagle/internal/access/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

type permissionRepoStub struct {
	domain.PermissionRepo
	permission *domain.Permission
	query      domain.ListPermissionsQuery
	deletedID  int64
	revision   *int64
	err        error
}

func (r *permissionRepoStub) Create(context.Context, *domain.Permission) (*domain.Permission, error) {
	return r.permission, r.err
}
func (r *permissionRepoStub) GetByID(context.Context, int64) (*domain.Permission, error) {
	return r.permission, r.err
}
func (r *permissionRepoStub) List(_ context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	r.query = q
	if r.err != nil {
		return nil, r.err
	}
	return []*domain.Permission{r.permission}, nil
}
func (r *permissionRepoStub) Update(_ context.Context, p *domain.Permission, revision *int64) (*domain.Permission, error) {
	r.permission, r.revision = p, revision
	return p, r.err
}
func (r *permissionRepoStub) Delete(_ context.Context, id int64, revision *int64) error {
	r.deletedID, r.revision = id, revision
	return r.err
}

type policyRepoStub struct {
	domain.PolicyRepo
	binding     *domain.RoleBinding
	bindings    []*domain.RoleBinding
	codes       []domain.PermissionCode
	inheritance []domain.RoleInheritance
	version     int64
	err         error
}

func (r *policyRepoStub) FindBinding(context.Context, domain.Role) (*domain.RoleBinding, error) {
	return r.binding, r.err
}
func (r *policyRepoStub) SaveBinding(_ context.Context, binding *domain.RoleBinding, _ *int64) (int64, error) {
	r.binding = binding
	return r.version, r.err
}
func (r *policyRepoStub) ListBindings(context.Context) ([]*domain.RoleBinding, int64, error) {
	return r.bindings, r.version, r.err
}
func (r *policyRepoStub) ResolveCodes(context.Context, []domain.Role) ([]domain.PermissionCode, error) {
	return r.codes, r.err
}
func (r *policyRepoStub) SaveInheritance(_ context.Context, ri domain.RoleInheritance, _ *int64) (int64, error) {
	r.inheritance = []domain.RoleInheritance{ri}
	return r.version, r.err
}
func (r *policyRepoStub) ListInheritances(context.Context) ([]domain.RoleInheritance, int64, error) {
	return r.inheritance, r.version, r.err
}
func (r *policyRepoStub) DeleteInheritance(_ context.Context, ri domain.RoleInheritance, _ *int64) (int64, error) {
	r.inheritance = []domain.RoleInheritance{ri}
	return r.version, r.err
}

func testPermission(t *testing.T) *domain.Permission {
	t.Helper()
	created := time.Unix(1_700_000_000, 0).UTC()
	p, err := domain.RehydratePermission(domain.PermissionSnapshot{
		ID: 42, ParentID: 1, Name: "用户管理", Code: "system:user:list",
		Type: int32(domain.PermissionTypeMenu), Path: "/users", Component: "UserList",
		Icon: "users", Sort: 3, Visible: true, Status: int32(domain.StatusEnabled),
		CreatedAt: created, UpdatedAt: created.Add(time.Minute), Revision: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPermissionConversions(t *testing.T) {
	if toProtoPermission(nil) != nil || ts(time.Time{}) != nil || toStatusPtr(nil) != nil || toPermissionTypePtr(nil) != nil {
		t.Fatal("nil and zero conversions must stay nil")
	}
	p := testPermission(t)
	got := toProtoPermission(p)
	if got.GetId() != 42 || got.GetParentId() != 1 || got.GetName() != "用户管理" ||
		got.GetCode() != "system:user:list" || got.GetType() != int32(domain.PermissionTypeMenu) ||
		got.GetPath() != "/users" || got.GetComponent() != "UserList" || got.GetIcon() != "users" ||
		got.GetSort() != 3 || !got.GetVisible() || got.GetStatus() != int32(domain.StatusEnabled) ||
		got.GetCreatedAt() == nil || got.GetUpdatedAt() == nil || got.GetRevision() != 7 {
		t.Fatalf("proto permission = %+v", got)
	}
	status, typ := int32(1), int32(2)
	if *toStatusPtr(&status) != domain.StatusEnabled || *toPermissionTypePtr(&typ) != domain.PermissionTypeMenu ||
		fromStatus(domain.StatusEnabled) != 1 {
		t.Fatal("scalar conversion mismatch")
	}
}

func TestPermissionServiceCRUD(t *testing.T) {
	repo := &permissionRepoStub{permission: testPermission(t)}
	service := NewPermissionService(application.NewPermissionUsecase(repo, &policyRepoStub{}))
	ctx := context.Background()
	revision := int64(7)
	write := &v1.CreatePermissionRequest{
		ParentId: 1, Name: "用户管理", Code: "system:user:list",
		Type: 2, Path: "/users", Component: "UserList", Icon: "users",
		Sort: 3, Visible: true, Status: 1,
	}
	created, err := service.CreatePermission(ctx, write)
	if err != nil || created.GetPermission().GetId() != 42 {
		t.Fatalf("create = %+v, %v", created, err)
	}
	got, err := service.GetPermission(ctx, &v1.GetPermissionRequest{Id: 42})
	if err != nil || got.GetPermission().GetId() != 42 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	status, typ := int32(1), int32(2)
	listed, err := service.ListPermissions(ctx, &v1.ListPermissionsRequest{Status: &status, Type: &typ})
	if err != nil || len(listed.GetPermissions()) != 1 || repo.query.Status == nil || repo.query.Type == nil {
		t.Fatalf("list = %+v, query=%+v, err=%v", listed, repo.query, err)
	}
	updated, err := service.UpdatePermission(ctx, &v1.UpdatePermissionRequest{
		Id: 42, ParentId: 1, Name: "用户列表", Code: "system:user:list", Type: 2,
		Path: "/users", Component: "UserList", Icon: "users", Sort: 4,
		Visible: true, Status: 1, ExpectedRevision: &revision,
	})
	if err != nil || updated.GetPermission().GetName() != "用户列表" || repo.revision == nil || *repo.revision != revision {
		t.Fatalf("update = %+v, revision=%v, err=%v", updated, repo.revision, err)
	}
	if _, err := service.DeletePermission(ctx, &v1.DeletePermissionRequest{Id: 42, ExpectedRevision: &revision}); err != nil || repo.deletedID != 42 {
		t.Fatalf("delete id=%d, err=%v", repo.deletedID, err)
	}
}

func TestPermissionServicePropagatesRepositoryFailure(t *testing.T) {
	want := errors.New("repository failed")
	repo := &permissionRepoStub{permission: testPermission(t), err: want}
	service := NewPermissionService(application.NewPermissionUsecase(repo, &policyRepoStub{}))
	if response, err := service.GetPermission(context.Background(), &v1.GetPermissionRequest{Id: 42}); response != nil || !errors.Is(err, want) {
		t.Fatalf("get = %+v, %v", response, err)
	}
	if response, err := service.ListPermissions(context.Background(), &v1.ListPermissionsRequest{}); response != nil || !errors.Is(err, want) {
		t.Fatalf("list = %+v, %v", response, err)
	}
	if response, err := service.DeletePermission(context.Background(), &v1.DeletePermissionRequest{Id: 42}); response != nil || !errors.Is(err, want) {
		t.Fatalf("delete = %+v, %v", response, err)
	}
}

func TestGetMyMenusUsesAuthenticatedRoles(t *testing.T) {
	permission := testPermission(t)
	code := domain.MustPermissionCode("system:user:list")
	repo := &permissionRepoStub{permission: permission}
	policy := &policyRepoStub{codes: []domain.PermissionCode{code}}
	service := NewPermissionService(application.NewPermissionUsecase(repo, policy))
	if response, err := service.GetMyMenus(context.Background(), &v1.GetMyMenusRequest{}); response != nil || kratoserrors.Code(err) != 401 {
		t.Fatalf("anonymous = %+v, %v", response, err)
	}
	ctx := identity.NewContext(context.Background(), &identity.Principal{Roles: []string{"viewer"}})
	got, err := service.GetMyMenus(ctx, &v1.GetMyMenusRequest{})
	if err != nil || len(got.GetMenus()) != 1 || !slices.Equal(got.GetPermissionCodes(), []string{"system:user:list"}) {
		t.Fatalf("menus = %+v, %v", got, err)
	}
}

func TestRoleBindingService(t *testing.T) {
	role, _ := domain.NewRole("viewer")
	binding, _ := domain.NewRoleBinding(role, []domain.PermissionCode{domain.MustPermissionCode("system:user:list")})
	binding = binding.WithRevision(4)
	inheritance, _ := domain.NewRoleInheritance(domain.Role("lead"), domain.Role("viewer"))
	policy := &policyRepoStub{
		binding: binding, bindings: []*domain.RoleBinding{binding},
		codes:       []domain.PermissionCode{domain.MustPermissionCode("system:user:list")},
		inheritance: []domain.RoleInheritance{inheritance}, version: 9,
	}
	service := NewRoleBindingService(application.NewRoleBindingUsecase(policy))
	ctx := context.Background()
	if toProtoBinding(nil) != nil {
		t.Fatal("nil binding must stay nil")
	}
	if got := toProtoBinding(binding); got.GetRole() != "viewer" || got.GetRevision() != 4 {
		t.Fatalf("binding = %+v", got)
	}
	listed, err := service.ListBoundRoles(ctx, &v1.ListBoundRolesRequest{})
	if err != nil || len(listed.GetBindings()) != 1 || listed.GetPolicyVersion() != 9 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	got, err := service.GetRolePermissions(ctx, &v1.GetRolePermissionsRequest{Role: "viewer"})
	if err != nil || got.GetBinding().GetRole() != "viewer" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	version := int64(8)
	set, err := service.SetRolePermissions(ctx, &v1.SetRolePermissionsRequest{
		Role: "viewer", PermissionCodes: []string{"system:user:list"}, ExpectedVersion: &version,
	})
	if err != nil || set.GetPolicyVersion() != 9 {
		t.Fatalf("set = %+v, %v", set, err)
	}
	added, err := service.AddRoleInheritance(ctx, &v1.AddRoleInheritanceRequest{Child: "lead", Parent: "viewer", ExpectedVersion: &version})
	if err != nil || added.GetPolicyVersion() != 9 {
		t.Fatalf("add inheritance = %+v, %v", added, err)
	}
	inherited, err := service.ListRoleInheritances(ctx, &v1.ListRoleInheritancesRequest{})
	if err != nil || len(inherited.GetInheritances()) != 1 || inherited.GetPolicyVersion() != 9 {
		t.Fatalf("list inheritance = %+v, %v", inherited, err)
	}
	deleted, err := service.DeleteRoleInheritance(ctx, &v1.DeleteRoleInheritanceRequest{Child: "lead", Parent: "viewer", ExpectedVersion: &version})
	if err != nil || deleted.GetPolicyVersion() != 9 {
		t.Fatalf("delete inheritance = %+v, %v", deleted, err)
	}
}

func TestGetMyPermissionsUsesAuthenticatedRoles(t *testing.T) {
	policy := &policyRepoStub{codes: []domain.PermissionCode{domain.MustPermissionCode("system:user:list")}}
	service := NewRoleBindingService(application.NewRoleBindingUsecase(policy))
	if response, err := service.GetMyPermissions(context.Background(), &v1.GetMyPermissionsRequest{}); response != nil || kratoserrors.Code(err) != 401 {
		t.Fatalf("anonymous = %+v, %v", response, err)
	}
	ctx := identity.NewContext(context.Background(), &identity.Principal{Roles: []string{"viewer"}})
	got, err := service.GetMyPermissions(ctx, &v1.GetMyPermissionsRequest{})
	if err != nil || !slices.Equal(got.GetRoles(), []string{"viewer"}) ||
		!slices.Equal(got.GetPermissionCodes(), []string{"system:user:list"}) {
		t.Fatalf("permissions = %+v, %v", got, err)
	}
}
