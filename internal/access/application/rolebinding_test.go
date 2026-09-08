package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eagle-go/eagle/internal/access/domain"
)

type policyRecorder struct {
	domain.PolicyRepo
	findBinding       func(context.Context, domain.Role) (*domain.RoleBinding, error)
	saveBinding       func(context.Context, *domain.RoleBinding, *int64) (int64, error)
	listBindings      func(context.Context) ([]*domain.RoleBinding, int64, error)
	resolveCodes      func(context.Context, []domain.Role) ([]domain.PermissionCode, error)
	saveInheritance   func(context.Context, domain.RoleInheritance, *int64) (int64, error)
	listInheritances  func(context.Context) ([]domain.RoleInheritance, int64, error)
	deleteInheritance func(context.Context, domain.RoleInheritance, *int64) (int64, error)
}

func (p policyRecorder) FindBinding(ctx context.Context, role domain.Role) (*domain.RoleBinding, error) {
	return p.findBinding(ctx, role)
}
func (p policyRecorder) SaveBinding(ctx context.Context, b *domain.RoleBinding, version *int64) (int64, error) {
	return p.saveBinding(ctx, b, version)
}
func (p policyRecorder) ListBindings(ctx context.Context) ([]*domain.RoleBinding, int64, error) {
	return p.listBindings(ctx)
}
func (p policyRecorder) ResolveCodes(ctx context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
	return p.resolveCodes(ctx, roles)
}
func (p policyRecorder) SaveInheritance(ctx context.Context, ri domain.RoleInheritance, version *int64) (int64, error) {
	return p.saveInheritance(ctx, ri, version)
}
func (p policyRecorder) ListInheritances(ctx context.Context) ([]domain.RoleInheritance, int64, error) {
	return p.listInheritances(ctx)
}
func (p policyRecorder) DeleteInheritance(ctx context.Context, ri domain.RoleInheritance, version *int64) (int64, error) {
	return p.deleteInheritance(ctx, ri, version)
}

func TestRoleBindingUsecaseQueries(t *testing.T) {
	role, _ := domain.NewRole("viewer")
	binding, _ := domain.NewRoleBinding(role, []domain.PermissionCode{domain.MustPermissionCode("system:user:list")})
	inheritance, _ := domain.NewRoleInheritance(role, domain.Role("user"))
	policy := policyRecorder{
		listBindings: func(context.Context) ([]*domain.RoleBinding, int64, error) {
			return []*domain.RoleBinding{binding}, 5, nil
		},
		findBinding: func(_ context.Context, got domain.Role) (*domain.RoleBinding, error) {
			if got != role {
				t.Fatalf("role = %q", got)
			}
			return binding, nil
		},
		listInheritances: func(context.Context) ([]domain.RoleInheritance, int64, error) {
			return []domain.RoleInheritance{inheritance}, 6, nil
		},
	}
	uc := NewRoleBindingUsecase(policy)
	if got, version, err := uc.ListBoundRoles(context.Background()); err != nil || version != 5 || len(got) != 1 {
		t.Fatalf("list bindings = %v, %d, %v", got, version, err)
	}
	if got, err := uc.GetRolePermissions(context.Background(), "viewer"); err != nil || got != binding {
		t.Fatalf("find binding = %v, %v", got, err)
	}
	if got, version, err := uc.ListRoleInheritances(context.Background()); err != nil || version != 6 || len(got) != 1 {
		t.Fatalf("list inheritances = %v, %d, %v", got, version, err)
	}
}

func TestGetRolePermissionsBoundaries(t *testing.T) {
	uc := NewRoleBindingUsecase(policyRecorder{})
	if _, err := uc.GetRolePermissions(context.Background(), "bad role"); !errors.Is(err, domain.ErrEmptyRole) {
		t.Fatalf("invalid role error = %v", err)
	}

	want := errors.New("load failed")
	uc = NewRoleBindingUsecase(policyRecorder{findBinding: func(context.Context, domain.Role) (*domain.RoleBinding, error) {
		return nil, want
	}})
	if _, err := uc.GetRolePermissions(context.Background(), "viewer"); !errors.Is(err, want) {
		t.Fatalf("load error = %v", err)
	}

	empty, _ := domain.NewRoleBinding(domain.Role("viewer"), nil)
	uc = NewRoleBindingUsecase(policyRecorder{findBinding: func(context.Context, domain.Role) (*domain.RoleBinding, error) {
		return empty, nil
	}})
	if _, err := uc.GetRolePermissions(context.Background(), "viewer"); !errors.Is(err, domain.ErrRoleNotBound) {
		t.Fatalf("empty binding error = %v", err)
	}
}

func TestSetRolePermissionsValidatesAndSaves(t *testing.T) {
	version := int64(4)
	policy := policyRecorder{saveBinding: func(_ context.Context, b *domain.RoleBinding, got *int64) (int64, error) {
		if b.Role().String() != "editor" || !slices.Equal(b.CodeStrings(), []string{"system:user:add", "system:user:list"}) {
			t.Fatalf("binding = %q %v", b.Role(), b.CodeStrings())
		}
		if got == nil || *got != version {
			t.Fatalf("version = %v", got)
		}
		return 5, nil
	}}
	uc := NewRoleBindingUsecase(policy)
	got, err := uc.SetRolePermissions(context.Background(), "editor", []string{
		"system:user:list", "system:user:add", "system:user:list",
	}, &version)
	if err != nil || got != 5 {
		t.Fatalf("save = %d, %v", got, err)
	}
	if _, err := uc.SetRolePermissions(context.Background(), "bad role", nil, nil); !errors.Is(err, domain.ErrEmptyRole) {
		t.Fatalf("role error = %v", err)
	}
	if _, err := uc.SetRolePermissions(context.Background(), "editor", []string{"bad"}, nil); !errors.Is(err, domain.ErrInvalidPermissionCode) {
		t.Fatalf("code error = %v", err)
	}
}

func TestRoleInheritanceCommands(t *testing.T) {
	version := int64(9)
	want := domain.RoleInheritance{Child: domain.Role("lead"), Parent: domain.Role("editor")}
	policy := policyRecorder{
		saveInheritance: func(_ context.Context, got domain.RoleInheritance, expected *int64) (int64, error) {
			if got != want || expected == nil || *expected != version {
				t.Fatalf("save args = %+v, %v", got, expected)
			}
			return 10, nil
		},
		deleteInheritance: func(_ context.Context, got domain.RoleInheritance, expected *int64) (int64, error) {
			if got != want || expected == nil || *expected != version {
				t.Fatalf("delete args = %+v, %v", got, expected)
			}
			return 11, nil
		},
	}
	uc := NewRoleBindingUsecase(policy)
	if got, err := uc.AddRoleInheritance(context.Background(), "lead", "editor", &version); err != nil || got != 10 {
		t.Fatalf("add = %d, %v", got, err)
	}
	if got, err := uc.DeleteRoleInheritance(context.Background(), "lead", "editor", &version); err != nil || got != 11 {
		t.Fatalf("delete = %d, %v", got, err)
	}
	for _, call := range []func() error{
		func() error {
			_, err := uc.AddRoleInheritance(context.Background(), "bad role", "editor", nil)
			return err
		},
		func() error {
			_, err := uc.AddRoleInheritance(context.Background(), "lead", "bad role", nil)
			return err
		},
		func() error { _, err := uc.AddRoleInheritance(context.Background(), "lead", "lead", nil); return err },
		func() error {
			_, err := uc.DeleteRoleInheritance(context.Background(), "bad role", "editor", nil)
			return err
		},
		func() error {
			_, err := uc.DeleteRoleInheritance(context.Background(), "lead", "bad role", nil)
			return err
		},
		func() error {
			_, err := uc.DeleteRoleInheritance(context.Background(), "lead", "lead", nil)
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatal("invalid inheritance unexpectedly succeeded")
		}
	}
}

func TestResolveCodesNormalizesRoles(t *testing.T) {
	want := []domain.PermissionCode{domain.MustPermissionCode("system:user:list")}
	uc := NewRoleBindingUsecase(policyRecorder{resolveCodes: func(_ context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
		if !slices.Equal(roles, []domain.Role{"viewer", "editor"}) {
			t.Fatalf("roles = %v", roles)
		}
		return want, nil
	}})
	got, err := uc.ResolveCodes(context.Background(), []string{"viewer", "", "editor"})
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("resolve = %v, %v", got, err)
	}
	if _, err := uc.ResolveCodes(context.Background(), []string{"bad role"}); !errors.Is(err, domain.ErrEmptyRole) {
		t.Fatalf("invalid role error = %v", err)
	}
}
