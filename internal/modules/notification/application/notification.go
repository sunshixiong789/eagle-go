// Package application coordinates notification use cases.
package application

import (
	"context"
	"time"

	"github.com/eagle-go/eagle/internal/modules/notification/domain"
)

type Usecase struct{ repo domain.Repository }

func NewUsecase(repo domain.Repository) *Usecase { return &Usecase{repo: repo} }

func (uc *Usecase) Send(ctx context.Context, sender, recipient, title, content string) (*domain.Notification, error) {
	return uc.repo.Create(ctx, &domain.Notification{
		SenderSubject: sender, RecipientSubject: recipient,
		Title: title, Content: content,
	})
}

func (uc *Usecase) List(ctx context.Context, q domain.ListQuery) ([]*domain.Notification, int64, error) {
	return uc.repo.List(ctx, q)
}

func (uc *Usecase) UnreadCount(ctx context.Context, recipient string) (int64, error) {
	return uc.repo.UnreadCount(ctx, recipient)
}

func (uc *Usecase) MarkRead(ctx context.Context, recipient string, id int64) (*domain.Notification, error) {
	return uc.repo.MarkRead(ctx, recipient, id, time.Now())
}

func (uc *Usecase) MarkAllRead(ctx context.Context, recipient string) (int64, error) {
	return uc.repo.MarkAllRead(ctx, recipient, time.Now())
}
