package model

import (
	"time"
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
	OrderID        string `db:"order_id"`
	LineNo         int    `db:"line_no"`
	SKU            string `db:"sku"`
	Qty            int64  `db:"qty"`
	UnitPriceCents int64  `db:"unit_price_cents"`
	DiscountBPS    int64  `db:"discount_bps"`
}
