package application

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/internal/access/domain"
)

type permissionFake struct {
	domain.PermissionRepo
	create func(context.Context, *domain.Permission) (*domain.Permission, error)
	get    func(context.Context, int64) (*domain.Permission, error)
	update func(context.Context, *domain.Permission, *int64) (*domain.Permission, error)
	list   func(context.Context, domain.ListPermissionsQuery) ([]*domain.Permission, error)
	delete func(context.Context, int64, *int64) error
}

func (r permissionFake) Create(ctx context.Context, p *domain.Permission) (*domain.Permission, error) {
	return r.create(ctx, p)
}
func (r permissionFake) GetByID(ctx context.Context, id int64) (*domain.Permission, error) {
	return r.get(ctx, id)
}
func (r permissionFake) Update(ctx context.Context, p *domain.Permission, v *int64) (*domain.Permission, error) {
	return r.update(ctx, p, v)
}
func (r permissionFake) List(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	return r.list(ctx, q)
}
func (r permissionFake) Delete(ctx context.Context, id int64, revision *int64) error {
	return r.delete(ctx, id, revision)
}

type policyFake struct {
	domain.PolicyRepo
	resolve func(context.Context, []domain.Role) ([]domain.PermissionCode, error)
}

func (p policyFake) ResolveCodes(ctx context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
	return p.resolve(ctx, roles)
}

func TestPermissionUsecaseCRUD(t *testing.T) {
	ctx := context.Background()
	revision := int64(7)
	wantErr := errors.New("repository unavailable")
	params := domain.NewPermissionParams{
		Name: "用户管理", Code: "system:user:list",
		Type: int32(domain.PermissionTypeMenu), Status: int32(domain.StatusEnabled),
	}

	t.Run("create validates before persistence", func(t *testing.T) {
		called := false
		repo := permissionFake{create: func(_ context.Context, p *domain.Permission) (*domain.Permission, error) {
			called = true
			if p.Name() != params.Name {
				t.Fatalf("name = %q", p.Name())
			}
			return p, nil
		}}
		got, err := NewPermissionUsecase(repo, nil).CreatePermission(ctx, params)
		if err != nil || got == nil || !called {
			t.Fatalf("create = %+v, %v; called=%v", got, err, called)
		}

		bad := params
		bad.Name = ""
		called = false
		got, err = NewPermissionUsecase(repo, nil).CreatePermission(ctx, bad)
		if got != nil || !errors.Is(err, domain.ErrEmptyPermissionName) || called {
			t.Fatalf("invalid create = %+v, %v; called=%v", got, err, called)
		}
	})

	t.Run("queries and delete preserve arguments and errors", func(t *testing.T) {
		status := domain.StatusEnabled
		query := domain.ListPermissionsQuery{Status: &status}
		repo := permissionFake{
			get: func(_ context.Context, id int64) (*domain.Permission, error) {
				if id != 42 {
					t.Fatalf("id = %d", id)
				}
				return nil, wantErr
			},
			list: func(_ context.Context, got domain.ListPermissionsQuery) ([]*domain.Permission, error) {
				if got.Status == nil || *got.Status != status {
					t.Fatalf("query = %+v", got)
				}
				return nil, wantErr
			},
			delete: func(_ context.Context, id int64, got *int64) error {
				if id != 42 || got == nil || *got != revision {
					t.Fatalf("delete args = %d, %v", id, got)
				}
				return wantErr
			},
		}
		uc := NewPermissionUsecase(repo, nil)
		if _, err := uc.GetPermission(ctx, 42); !errors.Is(err, wantErr) {
			t.Fatalf("get error = %v", err)
		}
		if _, err := uc.ListPermissions(ctx, query); !errors.Is(err, wantErr) {
			t.Fatalf("list error = %v", err)
		}
		if err := uc.DeletePermission(ctx, 42, &revision); !errors.Is(err, wantErr) {
			t.Fatalf("delete error = %v", err)
		}
	})
}

func TestUpdatePermissionFailureBoundaries(t *testing.T) {
	for _, stage := range []string{"load", "validation", "save"} {
		t.Run(stage, func(t *testing.T) {
			params := domain.NewPermissionParams{Name: "original", Type: int32(domain.PermissionTypeDir), Status: 1}
			current, err := domain.NewPermission(params)
			if err != nil {
				t.Fatal(err)
			}
			revision := int64(7)
			want := domain.ErrConcurrentModification
			repo := permissionFake{get: func(_ context.Context, id int64) (*domain.Permission, error) {
				if id != 42 {
					t.Fatalf("id = %d", id)
				}
				if stage == "load" {
					return nil, domain.ErrPermissionNotFound
				}
				return current, nil
			}, update: func(_ context.Context, p *domain.Permission, v *int64) (*domain.Permission, error) {
				if stage != "save" {
					t.Fatal("saved after prior failure")
				}
				if p.Name() != "updated" || v == nil || *v != revision {
					t.Fatal("lost update or revision")
				}
				return nil, domain.ErrConcurrentModification
			}}
			params.Name = "updated"
			if stage == "load" {
				want = domain.ErrPermissionNotFound
			}
			if stage == "validation" {
				params.Name = ""
				want = domain.ErrEmptyPermissionName
			}
			got, err := NewPermissionUsecase(repo, nil).UpdatePermission(context.Background(), 42, params, &revision)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("update = %+v, %v", got, err)
			}
		})
	}
}
func TestMenuResolutionStopsOnPolicyFailure(t *testing.T) {
	want := errors.New("policy unavailable")
	uc := NewPermissionUsecase(permissionFake{}, policyFake{resolve: func(context.Context, []domain.Role) ([]domain.PermissionCode, error) {
		return nil, want
	}})
	menus, codes, err := uc.GetMenusForRoles(context.Background(), []string{"user"})
	if menus != nil || codes != nil || !errors.Is(err, want) {
		t.Fatalf("menus = %v, %v, %v", menus, codes, err)
	}
}

func TestGetMenusForRoles(t *testing.T) {
	menu, err := domain.RehydratePermission(domain.PermissionSnapshot{
		ID: 1, Name: "用户管理", Code: "system:user:list",
		Type: int32(domain.PermissionTypeMenu), Status: int32(domain.StatusEnabled),
	})
	if err != nil {
		t.Fatal(err)
	}
	code := domain.MustPermissionCode("system:user:list")
	policy := policyFake{resolve: func(_ context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
		if len(roles) != 2 || roles[0].String() != "viewer" || roles[1].String() != "editor" {
			t.Fatalf("roles = %v", roles)
		}
		return []domain.PermissionCode{code}, nil
	}}
	repo := permissionFake{list: func(_ context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
		if q.Status != nil || q.Type != nil {
			t.Fatalf("query = %+v", q)
		}
		return []*domain.Permission{menu}, nil
	}}
	menus, codes, err := NewPermissionUsecase(repo, policy).GetMenusForRoles(
		context.Background(), []string{"viewer", "", "editor"},
	)
	if err != nil || len(menus) != 1 || menus[0].ID() != 1 || len(codes) != 1 || codes[0] != code {
		t.Fatalf("menus = %v, codes = %v, err = %v", menus, codes, err)
	}
}

func TestGetMenusForRolesRejectsInvalidRoleAndListFailure(t *testing.T) {
	resolveCalled := false
	policy := policyFake{resolve: func(context.Context, []domain.Role) ([]domain.PermissionCode, error) {
		resolveCalled = true
		return nil, nil
	}}
	uc := NewPermissionUsecase(permissionFake{}, policy)
	if _, _, err := uc.GetMenusForRoles(context.Background(), []string{"invalid role"}); !errors.Is(err, domain.ErrEmptyRole) {
		t.Fatalf("invalid role error = %v", err)
	}
	if resolveCalled {
		t.Fatal("policy called for invalid role")
	}

	want := errors.New("list failed")
	uc = NewPermissionUsecase(permissionFake{list: func(context.Context, domain.ListPermissionsQuery) ([]*domain.Permission, error) {
		return nil, want
	}}, policyFake{resolve: func(context.Context, []domain.Role) ([]domain.PermissionCode, error) {
		return []domain.PermissionCode{domain.MustPermissionCode("system:user:list")}, nil
	}})
	menus, codes, err := uc.GetMenusForRoles(context.Background(), []string{"viewer"})
	if menus != nil || codes != nil || !errors.Is(err, want) {
		t.Fatalf("result = %v, %v, %v", menus, codes, err)
	}
}
