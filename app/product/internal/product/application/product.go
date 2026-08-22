package application

import (
	"context"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

type Usecase struct{ repo domain.Repository }

func NewUsecase(repo domain.Repository) *Usecase { return &Usecase{repo: repo} }

func (uc *Usecase) Create(ctx context.Context, product *domain.Product) (*domain.Product, error) {
	return uc.repo.Create(ctx, product)
}

func (uc *Usecase) Get(ctx context.Context, id int64) (*domain.Product, error) {
	return uc.repo.Get(ctx, id)
}

func (uc *Usecase) BatchGet(ctx context.Context, ids []int64) ([]*domain.Product, error) {
	return uc.repo.BatchGet(ctx, ids)
}

func (uc *Usecase) List(ctx context.Context, page, pageSize int32, activeOnly bool) ([]*domain.Product, int64, error) {
	if pageSize == 0 {
		pageSize = 20
	}
	return uc.repo.List(ctx, domain.ListQuery{Offset: page * pageSize, PageSize: pageSize, ActiveOnly: activeOnly})
}

func (uc *Usecase) Update(ctx context.Context, product *domain.Product) (*domain.Product, error) {
	return uc.repo.Update(ctx, product)
}

func (uc *Usecase) Delete(ctx context.Context, id int64) error { return uc.repo.Delete(ctx, id) }
