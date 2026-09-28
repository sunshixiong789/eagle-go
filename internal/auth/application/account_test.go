package application

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

type accountFake struct {
	domain.AccountRepository
	query      domain.AccountQuery
	assignment domain.RoleAssignment
	bootstraps int
	err        error
}

func (f *accountFake) List(_ context.Context, q domain.AccountQuery) ([]domain.Account, int64, error) {
	f.query = q
	return []domain.Account{{Subject: "target"}}, 1, f.err
}
func (f *accountFake) Roles(context.Context, string) (*domain.AccountRoles, error) {
	return &domain.AccountRoles{Revision: 3}, f.err
}
func (f *accountFake) Replace(_ context.Context, a domain.RoleAssignment) (int64, error) {
	f.assignment = a
	return 4, f.err
}
func (f *accountFake) BootstrapAdmin(context.Context, string, string) error {
	f.bootstraps++
	return f.err
}

func TestAccountUsecase(t *testing.T) {
	ctx := context.Background()
	f := &accountFake{}
	uc := NewAccountUsecase(f)
	rows, total, err := uc.List(ctx, " x ", 0, 0)
	if err != nil || total != 1 || len(rows) != 1 || f.query.Keyword != "x" || f.query.Limit != 20 {
		t.Fatalf("query=%+v error=%v", f.query, err)
	}
	if _, _, err := uc.List(ctx, "", 2, 10); err != nil || f.query.Offset != 20 {
		t.Fatal("pagination lost")
	}
	if _, _, err := uc.List(ctx, "", 1, 10); err != nil || f.query.Offset != 10 {
		t.Fatal("second page repeats first page")
	}
	for _, q := range [][2]int32{{-1, 1}, {1000001, 1}, {1, -1}, {1, 101}} {
		if _, _, err := uc.List(ctx, "", q[0], q[1]); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
	if got, err := uc.Roles(ctx, "target"); err != nil || got.Revision != 3 {
		t.Fatal("roles lost")
	}
	if got, err := uc.Replace(ctx, "target", []string{"admin"}, 3, "actor"); err != nil || got != 4 || f.assignment.Actor != "actor" {
		t.Fatal("assignment lost")
	}
	if _, err := uc.Replace(ctx, "target", []string{"bad*"}, 3, "actor"); err == nil {
		t.Fatal("invalid roles accepted")
	}
	if err := uc.BootstrapAdmin(ctx, "", ""); err == nil || f.bootstraps != 0 {
		t.Fatal("invalid bootstrap reached repository")
	}
	f.err = domain.ErrAdminAlreadyInitialized
	if err := uc.BootstrapAdmin(ctx, "target", "actor"); !errors.Is(err, f.err) || f.bootstraps != 1 {
		t.Fatal("bootstrap error lost")
	}
}
