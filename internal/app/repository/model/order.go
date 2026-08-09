package model

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"zolo-test-integration/pkg/money"
)

// Order is a confirmed ZOLO order, the input to every tenant mapping.
type Order struct {
	OrderID             string    `db:"order_id"`
	TenantID            string    `db:"tenant_id"`
	Status              string    `db:"status"`
	ConfirmedAt         time.Time `db:"confirmed_at"`
	ConfirmedByUserID   string    `db:"confirmed_by_user_id"`
	Currency            string    `db:"currency"`
	CustomerPhone       *string   `db:"customer_phone"`
	CustomerExternalRef *string   `db:"customer_external_ref"`
	Items               []OrderItem
}

type OrderItem struct {
	OrderID        string       `db:"order_id"`
	LineNo         int          `db:"line_no"`
	SKU            string       `db:"sku"`
	Qty            int64        `db:"qty"`
	UnitPriceCents money.Amount `db:"unit_price_cents"`
	DiscountBPS    int64        `db:"discount_bps"`
}

// LineTotal discounts after multiplying by quantity and rounds once, which keeps
// 18.50 x 0.9 x 10 on 16650 instead of 16649.
func (i OrderItem) LineTotal() money.Amount {
	return i.UnitPriceCents.Mul(i.Qty).ApplyDiscount(i.DiscountBPS)
}

// Subtotal is the tax-exclusive sum of all line totals.
func (o Order) Subtotal() money.Amount {
	var total money.Amount
	for _, item := range o.Items {
		total += item.LineTotal()
	}
	return total
}

func (o Order) ExternalRef() string {
	if o.CustomerExternalRef == nil {
		return ""
	}
	return *o.CustomerExternalRef
}

func (o Order) Phone() string {
	if o.CustomerPhone == nil {
		return ""
	}
	return *o.CustomerPhone
}

// Age is time since the order taker confirmed it. ERP B refuses anything over 24 hours.
func (o Order) Age(now time.Time) time.Duration {
	return now.Sub(o.ConfirmedAt)
}

// IdempotencyKey a double-click, a browser retry and a manual re-sync all produce the same key,
// so the ERP and our claim row both see the repeat.
func IdempotencyKey(orderID, tenantID string) string {
	sum := sha256.Sum256([]byte(orderID + tenantID))
	return hex.EncodeToString(sum[:])
}
