package payload

import (
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/pkg/money"
)

// CreatedOrder echoes what was stored. The subtotal is included in both minor units and
// major units so the caller can see exactly how its decimals were scaled and rounded.
type CreatedOrder struct {
	OrderID       string       `json:"order_id"`
	TenantID      string       `json:"tenant_id"`
	Status        string       `json:"status"`
	LineCount     int          `json:"line_count"`
	SubtotalCents money.Amount `json:"subtotal_cents"`
	Subtotal      string       `json:"subtotal"`
	Currency      string       `json:"currency"`
}

func CreatedOrderFromModel(order model.Order) CreatedOrder {
	subtotal := order.Subtotal()

	return CreatedOrder{
		OrderID:       order.OrderID,
		TenantID:      order.TenantID,
		Status:        order.Status,
		LineCount:     len(order.Items),
		SubtotalCents: subtotal,
		Subtotal:      subtotal.String(),
		Currency:      order.Currency,
	}
}
