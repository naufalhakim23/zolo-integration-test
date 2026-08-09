package model_test

import (
	"testing"
	"time"

	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/pkg/money"
)

func TestOrderItemLineTotal(t *testing.T) {
	cases := []struct {
		name  string
		price money.Amount
		qty   int64
		bps   int64
		want  money.Amount
	}{
		{name: "18.50 x 10 less 10%", price: 1850, qty: 10, bps: 1000, want: 16650},
		{name: "no discount", price: 1850, qty: 10, want: 18500},
		{name: "single unit", price: 999, qty: 1, bps: 500, want: 949},
		{name: "zero quantity", price: 1850, qty: 0, bps: 1000, want: 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := model.OrderItem{UnitPriceCents: c.price, Qty: c.qty, DiscountBPS: c.bps}
			if got := item.LineTotal(); got != c.want {
				t.Errorf("LineTotal() = %d (%s), want %d", got, got, c.want)
			}
		})
	}
}

func TestOrderSubtotal(t *testing.T) {
	cases := []struct {
		name  string
		items []model.OrderItem
		want  money.Amount
	}{
		{name: "no items", want: 0},
		{
			name:  "single discounted line",
			items: []model.OrderItem{{UnitPriceCents: 1850, Qty: 10, DiscountBPS: 1000}},
			want:  16650,
		},
		{
			name: "mixed lines",
			items: []model.OrderItem{
				{UnitPriceCents: 1850, Qty: 10, DiscountBPS: 1000},
				{UnitPriceCents: 1000, Qty: 6},
			},
			want: 22650,
		},
		{
			// Each line rounds on its own, so the subtotal must match the stored line totals.
			name: "per-line rounding is stable",
			items: []model.OrderItem{
				{UnitPriceCents: 34, Qty: 1},
				{UnitPriceCents: 34, Qty: 1},
				{UnitPriceCents: 34, Qty: 1},
			},
			want: 102,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (model.Order{Items: c.items}).Subtotal(); got != c.want {
				t.Errorf("Subtotal() = %d (%s), want %d", got, got, c.want)
			}
		})
	}
}

func TestOrderOptionalCustomerFields(t *testing.T) {
	ref, phone := "PARTNER-42", "+60123456789"

	cases := []struct {
		name      string
		order     model.Order
		wantRef   string
		wantPhone string
	}{
		{name: "both set", order: model.Order{CustomerExternalRef: &ref, CustomerPhone: &phone}, wantRef: ref, wantPhone: phone},
		{name: "both nil"},
		{name: "ref only", order: model.Order{CustomerExternalRef: &ref}, wantRef: ref},
		{name: "phone only", order: model.Order{CustomerPhone: &phone}, wantPhone: phone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.order.ExternalRef(); got != c.wantRef {
				t.Errorf("ExternalRef() = %q, want %q", got, c.wantRef)
			}
			if got := c.order.Phone(); got != c.wantPhone {
				t.Errorf("Phone() = %q, want %q", got, c.wantPhone)
			}
		})
	}
}

func TestOrderAge(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		confirmedAt time.Time
		want        time.Duration
	}{
		{name: "just confirmed", confirmedAt: now, want: 0},
		{name: "one hour old", confirmedAt: now.Add(-time.Hour), want: time.Hour},
		{name: "exactly at the expiry boundary", confirmedAt: now.Add(-24 * time.Hour), want: 24 * time.Hour},
		{name: "clock skew reads negative", confirmedAt: now.Add(time.Minute), want: -time.Minute},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (model.Order{ConfirmedAt: c.confirmedAt}).Age(now); got != c.want {
				t.Errorf("Age() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIdempotencyKey(t *testing.T) {
	base := model.IdempotencyKey("ord_1", "tenant_alpha")

	cases := []struct {
		name     string
		orderID  string
		tenantID string
		wantSame bool
	}{
		{name: "same inputs repeat the key", orderID: "ord_1", tenantID: "tenant_alpha", wantSame: true},
		{name: "different tenant", orderID: "ord_1", tenantID: "tenant_beta"},
		{name: "different order", orderID: "ord_2", tenantID: "tenant_alpha"},
		// The ERP A contract hashes the concatenation with no separator, so a shifted
		// boundary collides. Harmless while order ids are "ord_"-prefixed and tenant ids
		// never start with a digit; changing it would stop matching the key ERP A computes.
		{name: "shifted boundary collides", orderID: "ord_", tenantID: "1tenant_alpha", wantSame: true},
	}

	if len(base) != 64 {
		t.Fatalf("key length = %d, want 64 hex chars", len(base))
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := model.IdempotencyKey(c.orderID, c.tenantID)
			if same := got == base; same != c.wantSame {
				t.Errorf("IdempotencyKey(%q, %q) same as base = %v, want %v", c.orderID, c.tenantID, same, c.wantSame)
			}
		})
	}
}
