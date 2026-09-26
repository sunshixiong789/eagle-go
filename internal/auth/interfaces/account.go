package interfaces

import (
	"context"

	v1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	"github.com/eagle-go/eagle/internal/auth/application"
	"github.com/eagle-go/eagle/pkg/identity"
)

// AccountService 转换账号管理协议，当前操作人由统一身份上下文传递。
type AccountService struct{ uc *application.AccountUsecase }

func NewAccountService(uc *application.AccountUsecase) *AccountService {
	return &AccountService{uc: uc}
}

// ListAccounts 分页返回统一账号。
func (s *AccountService) ListAccounts(ctx context.Context, req *v1.ListAccountsRequest) (*v1.ListAccountsResponse, error) {
	rows, total, err := s.uc.List(ctx, req.GetKeyword(), req.GetPage(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	out := &v1.ListAccountsResponse{Total: total}
	for _, row := range rows {
		out.Accounts = append(out.Accounts, &v1.Account{Subject: row.Subject, DisplayName: row.DisplayName, Status: row.Status})
	}
	return out, nil
}

// GetAccountRoles 返回当前应用角色与版本。
func (s *AccountService) GetAccountRoles(ctx context.Context, req *v1.GetAccountRolesRequest) (*v1.GetAccountRolesResponse, error) {
	roles, err := s.uc.Roles(ctx, req.GetSubject())
	if err != nil {
		return nil, err
	}
	return &v1.GetAccountRolesResponse{Roles: roles.Roles, Revision: roles.Revision}, nil
}

// SetAccountRoles 传递认证主体并全量更新账号角色。
func (s *AccountService) SetAccountRoles(ctx context.Context, req *v1.SetAccountRolesRequest) (*v1.SetAccountRolesResponse, error) {
	revision, err := s.uc.Replace(ctx, req.GetSubject(), req.GetRoles(), req.GetExpectedRevision(), identity.Subject(ctx))
	if err != nil {
		return nil, err
	}
	return &v1.SetAccountRolesResponse{Revision: revision}, nil
}
