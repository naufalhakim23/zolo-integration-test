package payload

import (
	"fmt"
	"strings"
	"time"

	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/money"
)

// MaxLineItems caps one order. The chunker turns a large order into a request per two
// lines for ERP A, so an unbounded list is an unbounded number of ERP round trips.
const MaxLineItems = 200

// CreateOrderRequest is the confirmed-order payload the ZOLO dashboard posts when an
// order taker clicks "Confirm Order". It is the only place major-unit decimals enter
// the system; everything downstream is integer minor units.
type CreateOrderRequest struct {
	OrderID           string    `json:"order_id"`
	TenantID          string    `json:"tenant_id"`
	ConfirmedAt       time.Time `json:"confirmed_at"`
	ConfirmedByUserID string    `json:"confirmed_by_user_id"`
	Currency          string    `json:"currency"`
	Customer          struct {
		Phone       string `json:"phone"`
		ExternalRef string `json:"external_ref"`
	} `json:"customer"`
	Items []CreateOrderItem `json:"items"`
}

type CreateOrderItem struct {
	SKU string `json:"sku"`
	Qty int64  `json:"qty"`

	// UnitPrice arrives as a major-unit decimal ("unit_price": 18.5) and is parsed as
	// text through big.Rat, never through a float64.
	UnitPrice money.Decimal `json:"unit_price"`

	// DiscountPercent is stored as basis points, which is what money.Decimal already
	// produces: the scale is 100, so 10 percent parses straight to 1000 bps.
	DiscountPercent money.Decimal `json:"discount_percent"`
}

const maxDiscountBPS = 10000

func (r *CreateOrderRequest) Validate() []string {
	var errs []string

	if strings.TrimSpace(r.OrderID) == "" {
		errs = append(errs, "order_id is required")
	}
	if strings.TrimSpace(r.TenantID) == "" {
		errs = append(errs, "tenant_id is required")
	}
	if r.ConfirmedAt.IsZero() {
		errs = append(errs, "confirmed_at is required")
	}
	if strings.TrimSpace(r.ConfirmedByUserID) == "" {
		errs = append(errs, "confirmed_by_user_id is required")
	}
	if !money.Supported(r.Currency) {
		errs = append(errs, fmt.Sprintf("currency %q is not a supported ISO-4217 code", r.Currency))
	}

	// Every tenant mapping needs one or the other: ERP A falls back to the phone when
	// external_ref is missing, ERP B needs the ref itself.
	if strings.TrimSpace(r.Customer.Phone) == "" && strings.TrimSpace(r.Customer.ExternalRef) == "" {
		errs = append(errs, "customer requires at least one of phone or external_ref")
	}

	switch {
	case len(r.Items) == 0:
		errs = append(errs, "items is required")
	case len(r.Items) > MaxLineItems:
		errs = append(errs, fmt.Sprintf("items must contain at most %d entries", MaxLineItems))
	}

	for i, item := range r.Items {
		if strings.TrimSpace(item.SKU) == "" {
			errs = append(errs, fmt.Sprintf("items[%d].sku is required", i))
		}
		if item.Qty <= 0 {
			errs = append(errs, fmt.Sprintf("items[%d].qty must be greater than zero", i))
		}
		if item.UnitPrice.Amount() < 0 {
			errs = append(errs, fmt.Sprintf("items[%d].unit_price cannot be negative", i))
		}
		if bps := int64(item.DiscountPercent); bps < 0 || bps > maxDiscountBPS {
			errs = append(errs, fmt.Sprintf("items[%d].discount_percent must be between 0 and 100", i))
		}
	}

	return errs
}

// ToModel converts a validated request into the domain order. Scaling happens here and
// nowhere else.
func (r *CreateOrderRequest) ToModel() model.Order {
	order := model.Order{
		OrderID:           strings.TrimSpace(r.OrderID),
		TenantID:          strings.TrimSpace(r.TenantID),
		Status:            pkg.OrderStatusConfirmed,
		ConfirmedAt:       r.ConfirmedAt.UTC(),
		ConfirmedByUserID: r.ConfirmedByUserID,
		Currency:          strings.ToUpper(strings.TrimSpace(r.Currency)),
	}

	if phone := strings.TrimSpace(r.Customer.Phone); phone != "" {
		order.CustomerPhone = &phone
	}
	if ref := strings.TrimSpace(r.Customer.ExternalRef); ref != "" {
		order.CustomerExternalRef = &ref
	}

	order.Items = make([]model.OrderItem, 0, len(r.Items))
	for i, item := range r.Items {
		order.Items = append(order.Items, model.OrderItem{
			OrderID:        order.OrderID,
			LineNo:         i + 1,
			SKU:            strings.TrimSpace(item.SKU),
			Qty:            item.Qty,
			UnitPriceCents: item.UnitPrice.Amount(),
			DiscountBPS:    int64(item.DiscountPercent),
		})
	}

	return order
}
