// Package infrastructure implements file persistence and blob storage.
package infrastructure

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/file"
)

type repository struct{ db *platformdb.Database }

func NewRepository(db *platformdb.Database) domain.Repository { return &repository{db: db} }

func toDomain(row *ent.File) *domain.File {
	if row == nil {
		return nil
	}
	return &domain.File{
		ID: row.ID, OwnerSubject: row.OwnerSubject, Name: row.Name,
		StorageKey: row.StorageKey, ContentType: row.ContentType,
		Size: row.Size, SHA256: row.Sha256, CreatedAt: row.CreatedAt,
	}
}

func (r *repository) Create(ctx context.Context, value *domain.File) (*domain.File, error) {
	row, err := r.db.Client().File.Create().
		SetID(value.ID).
		SetOwnerSubject(value.OwnerSubject).
		SetName(value.Name).
		SetStorageKey(value.StorageKey).
		SetContentType(value.ContentType).
		SetSize(value.Size).
		SetSha256(value.SHA256).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create file metadata: %w", err)
	}
	return toDomain(row), nil
}

func (r *repository) GetOwned(ctx context.Context, owner, id string) (*domain.File, error) {
	row, err := r.db.Client().File.Query().
		Where(file.IDEQ(id), file.OwnerSubjectEQ(owner)).
		Only(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrFileNotFound
		}
		return nil, fmt.Errorf("get file metadata: %w", err)
	}
	return toDomain(row), nil
}

func (r *repository) List(ctx context.Context, q domain.ListQuery) ([]*domain.File, int64, error) {
	query := r.db.Client().File.Query().Where(file.OwnerSubjectEQ(q.OwnerSubject))
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count files: %w", err)
	}
	rows, err := query.Order(ent.Desc(file.FieldCreatedAt)).Offset(int(q.Offset)).Limit(int(q.PageSize)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list files: %w", err)
	}
	out := make([]*domain.File, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomain(row))
	}
	return out, int64(total), nil
}

func (r *repository) DeleteOwned(ctx context.Context, owner, id string) error {
	deleted, err := r.db.Client().File.Delete().Where(file.IDEQ(id), file.OwnerSubjectEQ(owner)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete file metadata: %w", err)
	}
	if deleted == 0 {
		return domain.ErrFileNotFound
	}
	return nil
}
