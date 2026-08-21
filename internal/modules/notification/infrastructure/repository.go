// Package infrastructure implements notification persistence.
package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/notification"
	"github.com/eagle-go/eagle/internal/modules/notification/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
)

type repository struct{ db *platformdb.Database }

func NewRepository(db *platformdb.Database) domain.Repository { return &repository{db: db} }

func toDomain(row *ent.Notification) *domain.Notification {
	if row == nil {
		return nil
	}
	return &domain.Notification{
		ID: row.ID, RecipientSubject: row.RecipientSubject,
		SenderSubject: row.SenderSubject, Title: row.Title,
		Content: row.Content, ReadAt: row.ReadAt, CreatedAt: row.CreatedAt,
	}
}

func (r *repository) Create(ctx context.Context, value *domain.Notification) (*domain.Notification, error) {
	row, err := r.db.Client().Notification.Create().
		SetRecipientSubject(value.RecipientSubject).
		SetSenderSubject(value.SenderSubject).
		SetTitle(value.Title).
		SetContent(value.Content).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return toDomain(row), nil
}

func (r *repository) List(ctx context.Context, q domain.ListQuery) ([]*domain.Notification, int64, error) {
	query := r.db.Client().Notification.Query().Where(notification.RecipientSubjectEQ(q.RecipientSubject))
	if q.UnreadOnly {
		query = query.Where(notification.ReadAtIsNil())
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}
	rows, err := query.Order(ent.Desc(notification.FieldCreatedAt)).Offset(int(q.Offset)).Limit(int(q.PageSize)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	out := make([]*domain.Notification, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomain(row))
	}
	return out, int64(total), nil
}

func (r *repository) UnreadCount(ctx context.Context, recipient string) (int64, error) {
	count, err := r.db.Client().Notification.Query().Where(
		notification.RecipientSubjectEQ(recipient), notification.ReadAtIsNil(),
	).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return int64(count), nil
}

func (r *repository) MarkRead(ctx context.Context, recipient string, id int64, at time.Time) (*domain.Notification, error) {
	row, err := r.db.Client().Notification.Query().Where(
		notification.IDEQ(id), notification.RecipientSubjectEQ(recipient),
	).Only(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrNotificationNotFound
		}
		return nil, fmt.Errorf("get notification: %w", err)
	}
	if row.ReadAt != nil {
		return toDomain(row), nil
	}
	row, err = row.Update().SetReadAt(at).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark notification read: %w", err)
	}
	return toDomain(row), nil
}

func (r *repository) MarkAllRead(ctx context.Context, recipient string, at time.Time) (int64, error) {
	updated, err := r.db.Client().Notification.Update().Where(
		notification.RecipientSubjectEQ(recipient), notification.ReadAtIsNil(),
	).SetReadAt(at).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	return int64(updated), nil
}
