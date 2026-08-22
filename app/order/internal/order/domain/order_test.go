package domain

import (
	"errors"
	"testing"
)

func TestNewOrderSnapshotsPriceAndRejectsDuplicates(t *testing.T) {
	products := map[int64]ProductSnapshot{1: {ID: 1, SKU: "SKU-1", Name: "demo", PriceCents: 2500, Active: true}}
	order, err := New("subject", []RequestedItem{{ProductID: 1, Quantity: 2}}, products)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if order.TotalCents != 5000 || order.Items[0].UnitPriceCents != 2500 {
		t.Fatalf("unexpected price snapshot: %+v", order)
	}
	_, err = New("subject", []RequestedItem{{ProductID: 1, Quantity: 1}, {ProductID: 1, Quantity: 1}}, products)
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestNewOrderRejectsInactiveProduct(t *testing.T) {
	_, err := New("subject", []RequestedItem{{ProductID: 1, Quantity: 1}}, map[int64]ProductSnapshot{
		1: {ID: 1, PriceCents: 100, Active: false},
	})
	if !errors.Is(err, ErrProductUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
