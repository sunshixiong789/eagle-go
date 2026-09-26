package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/database/ent"
	"github.com/eagle-go/eagle/internal/platform/database/ent/accountrolestate"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useraccount"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useraudience"
	"github.com/eagle-go/eagle/internal/platform/database/ent/userrolebinding"
)

type accountRepo struct {
	db       *platformdb.Database
	audience string
}

func NewAccountRepository(db *platformdb.Database, audience string) domain.AccountRepository {
	return &accountRepo{db: db, audience: audience}
}

func (r *accountRepo) List(ctx context.Context, q domain.AccountQuery) ([]domain.Account, int64, error) {
	query := r.db.Client().UserAccount.Query()
	if q.Keyword != "" {
		query.Where(useraccount.Or(useraccount.IDEQ(q.Keyword), useraccount.DisplayNameContainsFold(q.Keyword)))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count accounts: %w", err)
	}
	rows, err := query.Order(ent.Asc(useraccount.FieldID)).Offset(q.Offset).Limit(q.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]domain.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Account{Subject: row.ID, DisplayName: row.DisplayName, Status: row.Status})
	}
	return out, int64(total), nil
}

func (r *accountRepo) Roles(ctx context.Context, subject string) (*domain.AccountRoles, error) {
	tx, err := r.db.Client().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := lockAccountRoleState(ctx, tx, r.audience)
	if err != nil {
		return nil, err
	}
	if _, err := tx.UserAccount.Get(ctx, subject); err != nil {
		return nil, accountError(err)
	}
	roles, err := loadRoles(ctx, tx, subject, r.audience)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &domain.AccountRoles{Roles: roles, Revision: state.Revision}, nil
}

func (r *accountRepo) Replace(ctx context.Context, assignment domain.RoleAssignment) (int64, error) {
	tx, err := r.db.Client().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// 和登录保持同样的锁顺序：目标账号 -> audience 状态行。READ COMMITTED 保证等待锁后读取最新管理员集合。
	account, err := tx.UserAccount.UpdateOneID(assignment.Subject).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return 0, accountError(err)
	}
	state, err := lockAccountRoleState(ctx, tx, r.audience)
	if err != nil {
		return 0, err
	}
	if state.Revision != assignment.ExpectedRevision {
		return 0, domain.ErrRoleRevisionConflict
	}
	before, err := loadRoles(ctx, tx, assignment.Subject, r.audience)
	if err != nil {
		return 0, err
	}
	if account.Status == accountStatusEnabled && slices.Contains(before, "admin") && !slices.Contains(assignment.Roles, "admin") {
		exists, err := otherEnabledAdmin(ctx, tx, r.audience, assignment.Subject)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, domain.ErrLastAdmin
		}
	}
	if err := markAudience(ctx, tx, assignment.Subject, r.audience); err != nil {
		return 0, err
	}
	if _, err := tx.UserRoleBinding.Delete().Where(userrolebinding.AccountSubjectEQ(assignment.Subject), userrolebinding.AudienceEQ(r.audience)).Exec(ctx); err != nil {
		return 0, err
	}
	for _, role := range assignment.Roles {
		if _, err := tx.UserRoleBinding.Create().SetAccountSubject(assignment.Subject).SetAudience(r.audience).SetRole(role).Save(ctx); err != nil {
			return 0, err
		}
	}
	revision, err := auditAccountRoles(ctx, tx, state, assignment.Subject, assignment.Actor, "roles.replace", before, assignment.Roles)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (r *accountRepo) BootstrapAdmin(ctx context.Context, subject, actor string) error {
	tx, err := r.db.Client().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	account, err := tx.UserAccount.UpdateOneID(subject).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return accountError(err)
	}
	if account.Status != accountStatusEnabled {
		return domain.ErrAccountDisabled
	}
	state, err := lockAccountRoleState(ctx, tx, r.audience)
	if err != nil {
		return err
	}
	exists, err := tx.UserRoleBinding.Query().Where(userrolebinding.AudienceEQ(r.audience), userrolebinding.RoleEQ("admin")).Exist(ctx)
	if err != nil {
		return err
	}
	if state.AdminInitialized || exists {
		return domain.ErrAdminAlreadyInitialized
	}
	before, err := loadRoles(ctx, tx, subject, r.audience)
	if err != nil {
		return err
	}
	if err := markAudience(ctx, tx, subject, r.audience); err != nil {
		return err
	}
	if _, err := tx.UserRoleBinding.Create().SetAccountSubject(subject).SetAudience(r.audience).SetRole("admin").Save(ctx); err != nil {
		return err
	}
	after := append(slices.Clone(before), "admin")
	slices.Sort(after)
	if _, err := auditAccountRoles(ctx, tx, state, subject, actor, "admin.bootstrap", before, after); err != nil {
		return err
	}
	if _, err := tx.AccountRoleState.UpdateOneID(r.audience).SetAdminInitialized(true).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func accountError(err error) error {
	if ent.IsNotFound(err) {
		return domain.ErrAccountNotFound
	}
	return fmt.Errorf("account storage: %w", err)
}

// lockAccountRoleState 使用唯一键幂等创建状态，再取得行写锁，串行化该 audience 的角色变更。
func lockAccountRoleState(ctx context.Context, tx *ent.Tx, audience string) (*ent.AccountRoleState, error) {
	if err := tx.AccountRoleState.Create().SetID(audience).OnConflictColumns(accountrolestate.FieldID).Ignore().Exec(ctx); err != nil {
		return nil, err
	}
	return tx.AccountRoleState.UpdateOneID(audience).SetUpdatedAt(time.Now()).Save(ctx)
}

func markAudience(ctx context.Context, tx *ent.Tx, subject, audience string) error {
	return tx.UserAudience.Create().SetAccountSubject(subject).SetAudience(audience).
		OnConflictColumns(useraudience.FieldAccountSubject, useraudience.FieldAudience).Ignore().Exec(ctx)
}

func loadRoles(ctx context.Context, tx *ent.Tx, subject, audience string) ([]string, error) {
	rows, err := tx.UserRoleBinding.Query().Where(userrolebinding.AccountSubjectEQ(subject), userrolebinding.AudienceEQ(audience)).All(ctx)
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.Role)
	}
	slices.Sort(roles)
	return roles, nil
}

func otherEnabledAdmin(ctx context.Context, tx *ent.Tx, audience, excluded string) (bool, error) {
	rows, err := tx.UserRoleBinding.Query().Where(userrolebinding.AudienceEQ(audience), userrolebinding.RoleEQ("admin"), userrolebinding.AccountSubjectNEQ(excluded)).All(ctx)
	if err != nil {
		return false, err
	}
	subjects := make([]string, 0, len(rows))
	for _, row := range rows {
		subjects = append(subjects, row.AccountSubject)
	}
	if len(subjects) == 0 {
		return false, nil
	}
	return tx.UserAccount.Query().Where(useraccount.IDIn(subjects...), useraccount.StatusEQ(accountStatusEnabled)).Exist(ctx)
}

// auditAccountRoles 推进版本和追加审计，调用方与角色写入一起提交；审计失败时整个角色变更回滚。
func auditAccountRoles(ctx context.Context, tx *ent.Tx, state *ent.AccountRoleState, subject, actor, action string, before, after []string) (int64, error) {
	revision := state.Revision + 1
	if before == nil {
		before = []string{}
	}
	if after == nil {
		after = []string{}
	}
	if _, err := tx.AccountRoleAudit.Create().SetAudience(state.ID).SetRevision(revision).SetAccountSubject(subject).SetActorSubject(actor).SetAction(action).SetBefore(before).SetAfter(after).Save(ctx); err != nil {
		return 0, err
	}
	if _, err := tx.AccountRoleState.UpdateOneID(state.ID).SetRevision(revision).Save(ctx); err != nil {
		return 0, err
	}
	return revision, nil
}
