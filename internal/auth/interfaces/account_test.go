package interfaces

import (
	"context"
	"testing"

	v1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	"github.com/eagle-go/eagle/internal/auth/application"
	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

type accountRepoFake struct {
	domain.AccountRepository
	query domain.AccountQuery
	actor string
	err   error
}

func (f *accountRepoFake) List(_ context.Context, q domain.AccountQuery) ([]domain.Account, int64, error) {
	f.query = q
	return []domain.Account{{Subject: "target", DisplayName: "Test", Status: 1}}, 1, f.err
}
func (f *accountRepoFake) Roles(context.Context, string) (*domain.AccountRoles, error) {
	return &domain.AccountRoles{Roles: []string{"user"}, Revision: 5}, f.err
}
func (f *accountRepoFake) Replace(_ context.Context, a domain.RoleAssignment) (int64, error) {
	f.actor = a.Actor
	return 6, f.err
}
func TestAccountServiceMappings(t *testing.T) {
	f := &accountRepoFake{}
	s := NewAccountService(application.NewAccountUsecase(f))
	ctx := identity.NewContext(context.Background(), &identity.Principal{Subject: "actor"})
	list, err := s.ListAccounts(ctx, &v1.ListAccountsRequest{})
	if err != nil || list.Total != 1 || list.Accounts[0].Subject != "target" {
		t.Fatal("list mapping failed")
	}
	roles, err := s.GetAccountRoles(ctx, &v1.GetAccountRolesRequest{Subject: "target"})
	if err != nil || roles.Revision != 5 || roles.Roles[0] != "user" {
		t.Fatal("role mapping failed")
	}
	updated, err := s.SetAccountRoles(ctx, &v1.SetAccountRolesRequest{Subject: "target", ExpectedRevision: 5})
	if err != nil || updated.Revision != 6 || f.actor != "actor" {
		t.Fatal("actor/version mapping failed")
	}
	f.err = domain.ErrAccountNotFound
	if _, err := s.ListAccounts(ctx, &v1.ListAccountsRequest{}); err == nil {
		t.Fatal("list error lost")
	}
	if _, err := s.GetAccountRoles(ctx, &v1.GetAccountRolesRequest{}); err == nil {
		t.Fatal("read error lost")
	}
	if _, err := s.SetAccountRoles(ctx, &v1.SetAccountRolesRequest{Subject: "target", ExpectedRevision: 5}); err == nil {
		t.Fatal("write error lost")
	}
}

// TestAccountPagination 验证协议页码直接使用统一的 0-based 契约。
func TestAccountPagination(t *testing.T) {
	for _, page := range []int32{0, 1, 2} {
		repo := &accountRepoFake{}
		service := NewAccountService(application.NewAccountUsecase(repo))
		_, err := service.ListAccounts(context.Background(), &v1.ListAccountsRequest{Page: page, PageSize: 10})
		if err != nil || repo.query.Offset != int64(page)*10 {
			t.Fatalf("page=%d query=%+v error=%v", page, repo.query, err)
		}
	}
}
