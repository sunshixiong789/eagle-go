package application

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/internal/access/domain"
)

type permissionFake struct {
	domain.PermissionRepo
	get    func(context.Context, int64) (*domain.Permission, error)
	update func(context.Context, *domain.Permission, *int64) (*domain.Permission, error)
	list   func(context.Context, domain.ListPermissionsQuery) ([]*domain.Permission, error)
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

type policyFake struct {
	domain.PolicyRepo
	err error
}

func (p policyFake) ResolveCodes(context.Context, []domain.Role) ([]domain.PermissionCode, error) {
	return nil, p.err
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
	uc := NewPermissionUsecase(permissionFake{}, policyFake{err: want})
	menus, codes, err := uc.GetMenusForRoles(context.Background(), []string{"realm:user"})
	if menus != nil || codes != nil || !errors.Is(err, want) {
		t.Fatalf("menus = %v, %v, %v", menus, codes, err)
	}
}
