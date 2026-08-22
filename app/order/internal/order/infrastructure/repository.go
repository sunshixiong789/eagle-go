package infrastructure

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/order/internal/order/domain"
	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/app/order/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/order/internal/platform/database/ent/orderitem"
	"github.com/eagle-go/eagle/app/order/internal/platform/database/ent/purchaseorder"
)

type repository struct{ db *platformdb.Database }

func NewRepository(db *platformdb.Database) domain.Repository { return &repository{db: db} }

func (r *repository) Create(ctx context.Context, value *domain.Order) (*domain.Order, error) {
	tx, err := r.db.Client().Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	row, err := tx.PurchaseOrder.Create().
		SetID(value.ID).SetOwnerSubject(value.OwnerSubject).SetStatus(value.Status).
		SetTotalCents(value.TotalCents).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	builders := make([]*ent.OrderItemCreate, 0, len(value.Items))
	for _, item := range value.Items {
		builders = append(builders, tx.OrderItem.Create().
			SetOrderID(value.ID).SetProductID(item.ProductID).SetProductSku(item.ProductSKU).
			SetProductName(item.ProductName).SetUnitPriceCents(item.UnitPriceCents).
			SetQuantity(item.Quantity).SetSubtotalCents(item.SubtotalCents))
	}
	if _, err := tx.OrderItem.CreateBulk(builders...).Save(ctx); err != nil {
		return nil, fmt.Errorf("create order items: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit order: %w", err)
	}
	value.CreatedAt = row.CreatedAt
	return value, nil
}

func (r *repository) GetOwned(ctx context.Context, owner, id string) (*domain.Order, error) {
	row, err := r.db.Client().PurchaseOrder.Query().Where(
		purchaseorder.IDEQ(id), purchaseorder.OwnerSubjectEQ(owner),
	).Only(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, fmt.Errorf("get order: %w", err)
	}
	return r.withItems(ctx, row)
}

func (r *repository) ListOwned(ctx context.Context, q domain.ListQuery) ([]*domain.Order, int64, error) {
	query := r.db.Client().PurchaseOrder.Query().Where(purchaseorder.OwnerSubjectEQ(q.OwnerSubject))
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}
	rows, err := query.Order(ent.Desc(purchaseorder.FieldCreatedAt)).Offset(int(q.Offset)).Limit(int(q.PageSize)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	if len(rows) == 0 {
		return []*domain.Order{}, int64(total), nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	itemRows, err := r.db.Client().OrderItem.Query().
		Where(orderitem.OrderIDIn(ids...)).Order(ent.Asc(orderitem.FieldID)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list order items: %w", err)
	}
	itemsByOrder := make(map[string][]domain.Item, len(rows))
	for _, item := range itemRows {
		itemsByOrder[item.OrderID] = append(itemsByOrder[item.OrderID], toDomainItem(item))
	}
	out := make([]*domain.Order, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainOrder(row, itemsByOrder[row.ID]))
	}
	return out, int64(total), nil
}

func (r *repository) withItems(ctx context.Context, row *ent.PurchaseOrder) (*domain.Order, error) {
	rows, err := r.db.Client().OrderItem.Query().Where(orderitem.OrderIDEQ(row.ID)).Order(ent.Asc(orderitem.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get order items: %w", err)
	}
	items := make([]domain.Item, 0, len(rows))
	for _, item := range rows {
		items = append(items, toDomainItem(item))
	}
	return toDomainOrder(row, items), nil
}

func toDomainItem(item *ent.OrderItem) domain.Item {
	return domain.Item{
		ProductID: item.ProductID, ProductSKU: item.ProductSku, ProductName: item.ProductName,
		UnitPriceCents: item.UnitPriceCents, Quantity: item.Quantity, SubtotalCents: item.SubtotalCents,
	}
}

func toDomainOrder(row *ent.PurchaseOrder, items []domain.Item) *domain.Order {
	return &domain.Order{
		ID: row.ID, OwnerSubject: row.OwnerSubject, Status: row.Status,
		TotalCents: row.TotalCents, Items: items, CreatedAt: row.CreatedAt,
	}
}
