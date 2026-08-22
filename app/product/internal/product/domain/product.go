package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrProductSKUDuplicated = errors.New("product SKU already exists")
)

type Product struct {
	ID          int64
	SKU         string
	Name        string
	Description string
	PriceCents  int64
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ListQuery struct {
	Offset     int32
	PageSize   int32
	ActiveOnly bool
}

type Repository interface {
	Create(context.Context, *Product) (*Product, error)
	Get(context.Context, int64) (*Product, error)
	BatchGet(context.Context, []int64) ([]*Product, error)
	List(context.Context, ListQuery) ([]*Product, int64, error)
	Update(context.Context, *Product) (*Product, error)
	Delete(context.Context, int64) error
}
