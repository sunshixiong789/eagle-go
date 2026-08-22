package domain

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

var (
	ErrOrderNotFound      = errors.New("order not found")
	ErrProductUnavailable = errors.New("product unavailable")
	ErrInvalidOrder       = errors.New("invalid order")
)

const StatusCreated = "created"

type ProductSnapshot struct {
	ID         int64
	SKU        string
	Name       string
	PriceCents int64
	Active     bool
}

type RequestedItem struct {
	ProductID int64
	Quantity  int32
}

type Item struct {
	ProductID      int64
	ProductSKU     string
	ProductName    string
	UnitPriceCents int64
	Quantity       int32
	SubtotalCents  int64
}

type Order struct {
	ID           string
	OwnerSubject string
	Status       string
	TotalCents   int64
	Items        []Item
	CreatedAt    time.Time
}

func New(owner string, requests []RequestedItem, products map[int64]ProductSnapshot) (*Order, error) {
	if owner == "" || len(requests) == 0 {
		return nil, ErrInvalidOrder
	}
	seen := make(map[int64]struct{}, len(requests))
	items := make([]Item, 0, len(requests))
	var total int64
	for _, requested := range requests {
		if requested.ProductID <= 0 || requested.Quantity <= 0 {
			return nil, ErrInvalidOrder
		}
		if _, exists := seen[requested.ProductID]; exists {
			return nil, fmt.Errorf("%w: duplicate product %d", ErrInvalidOrder, requested.ProductID)
		}
		seen[requested.ProductID] = struct{}{}
		product, ok := products[requested.ProductID]
		if !ok || !product.Active || product.PriceCents <= 0 {
			return nil, fmt.Errorf("%w: product %d", ErrProductUnavailable, requested.ProductID)
		}
		subtotal := product.PriceCents * int64(requested.Quantity)
		if subtotal/product.PriceCents != int64(requested.Quantity) || total > (1<<63-1)-subtotal {
			return nil, fmt.Errorf("%w: total overflow", ErrInvalidOrder)
		}
		total += subtotal
		items = append(items, Item{
			ProductID: product.ID, ProductSKU: product.SKU, ProductName: product.Name,
			UnitPriceCents: product.PriceCents, Quantity: requested.Quantity, SubtotalCents: subtotal,
		})
	}
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generate order id: %w", err)
	}
	return &Order{ID: id, OwnerSubject: owner, Status: StatusCreated, TotalCents: total, Items: items}, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

type ListQuery struct {
	OwnerSubject string
	Offset       int32
	PageSize     int32
}

type Repository interface {
	Create(context.Context, *Order) (*Order, error)
	GetOwned(context.Context, string, string) (*Order, error)
	ListOwned(context.Context, ListQuery) ([]*Order, int64, error)
}

type ProductCatalog interface {
	BatchGet(context.Context, []int64) ([]ProductSnapshot, error)
}
