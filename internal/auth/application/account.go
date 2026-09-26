package application

import (
	"context"
	"strings"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

// AccountUsecase 为 HTTP 角色管理与本地管理员初始化入口提供参数归一化和领域校验。
type AccountUsecase struct{ repo domain.AccountRepository }

func NewAccountUsecase(repo domain.AccountRepository) *AccountUsecase {
	return &AccountUsecase{repo: repo}
}

func (uc *AccountUsecase) List(ctx context.Context, keyword string, page, pageSize int32) ([]domain.Account, int64, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return nil, 0, domain.ErrInvalidRoleAssignment
	}
	return uc.repo.List(ctx, domain.AccountQuery{Keyword: strings.TrimSpace(keyword), Offset: int(page-1) * int(pageSize), Limit: int(pageSize)})
}
func (uc *AccountUsecase) Roles(ctx context.Context, subject string) (*domain.AccountRoles, error) {
	return uc.repo.Roles(ctx, subject)
}
func (uc *AccountUsecase) Replace(ctx context.Context, subject string, roles []string, revision int64, actor string) (int64, error) {
	assignment, err := domain.NewRoleAssignment(subject, roles, revision, actor)
	if err != nil {
		return 0, err
	}
	return uc.repo.Replace(ctx, assignment)
}

// BootstrapAdmin 验证显式指定的目标账号和操作人，再交给仓储原子完成一次性初始化。
func (uc *AccountUsecase) BootstrapAdmin(ctx context.Context, subject, actor string) error {
	if _, err := domain.NewRoleAssignment(subject, []string{"admin"}, 1, actor); err != nil {
		return err
	}
	return uc.repo.BootstrapAdmin(ctx, subject, actor)
}
