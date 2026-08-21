package payload_test

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/pkg/money"
)

func decode(t *testing.T, body string) payload.CreateOrderRequest {
	t.Helper()

	var req payload.CreateOrderRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return req
}

func itemBody(unitPrice, discountPercent string, qty int) string {
	return `{
		"order_id": "ord_1", "tenant_id": "tenant_alpha",
		"confirmed_at": "2026-07-22T08:00:00Z", "confirmed_by_user_id": "usr_1",
		"currency": "MYR", "customer": {"external_ref": "CUST-882"},
		"items": [{"sku": "SKU-1", "qty": ` + strconv.Itoa(qty) + `, "unit_price": ` + unitPrice + `, "discount_percent": ` + discountPercent + `}]
	}`
}

// Decimals from the dashboard are parsed as text, so the values that break a float64
// round trip land on the cent the arithmetic says they should.
func TestCreateOrderRequestScalesDecimals(t *testing.T) {
	cases := []struct {
		name            string
		unitPrice       string
		discountPercent string
		qty             int
		wantUnitCents   money.Amount
		wantBPS         int64
		wantLineTotal   money.Amount
	}{
		{
			name:            "brief example lands on 16650",
			unitPrice:       "18.5",
			discountPercent: "10",
			qty:             10,
			wantUnitCents:   1850,
			wantBPS:         1000,
			wantLineTotal:   16650,
		},
		{
			// 2.675 * 100 is 267.49999999999997 in float64, which truncates to 267.
			name:            "the classic float truncation case",
			unitPrice:       "2.675",
			discountPercent: "0",
			qty:             1,
			wantUnitCents:   268,
			wantBPS:         0,
			wantLineTotal:   268,
		},
		{
			name:            "fractional discount percent becomes exact basis points",
			unitPrice:       "10.00",
			discountPercent: "12.5",
			qty:             1,
			wantUnitCents:   1000,
			wantBPS:         1250,
			wantLineTotal:   875,
		},
		{
			name:            "repeating discount rounds once, at the line total",
			unitPrice:       "100.00",
			discountPercent: "33.33",
			qty:             1,
			wantUnitCents:   10000,
			wantBPS:         3333,
			wantLineTotal:   6667,
		},
		{
			name:            "full discount is free, not negative",
			unitPrice:       "19.99",
			discountPercent: "100",
			qty:             3,
			wantUnitCents:   1999,
			wantBPS:         10000,
			wantLineTotal:   0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := decode(t, itemBody(c.unitPrice, c.discountPercent, c.qty))

			if errs := req.Validate(); len(errs) > 0 {
				t.Fatalf("Validate() = %v, want none", errs)
			}

			item := req.ToModel().Items[0]

			if item.UnitPriceCents != c.wantUnitCents {
				t.Errorf("unit_price_cents = %d, want %d", item.UnitPriceCents, c.wantUnitCents)
			}
			if item.DiscountBPS != c.wantBPS {
				t.Errorf("discount_bps = %d, want %d", item.DiscountBPS, c.wantBPS)
			}
			if got := item.LineTotal(); got != c.wantLineTotal {
				t.Errorf("line total = %d, want %d", got, c.wantLineTotal)
			}
		})
	}
}

// Line numbers are assigned in payload order, which is what keeps ERP A's chunking
// deterministic across a retry.
func TestCreateOrderRequestAssignsLineNumbers(t *testing.T) {
	req := decode(t, `{
		"order_id": " ord_1 ", "tenant_id": "tenant_alpha",
		"confirmed_at": "2026-07-22T08:00:00Z", "confirmed_by_user_id": "usr_1",
		"currency": "myr", "customer": {"phone": "+60 12 345 6789"},
		"items": [
			{"sku": "SKU-A", "qty": 1, "unit_price": 1.00},
			{"sku": "SKU-B", "qty": 2, "unit_price": 2.00}
		]
	}`)

	order := req.ToModel()

	if order.OrderID != "ord_1" {
		t.Errorf("order_id = %q, want %q", order.OrderID, "ord_1")
	}
	if order.Currency != "MYR" {
		t.Errorf("currency = %q, want %q", order.Currency, "MYR")
	}
	if order.CustomerExternalRef != nil {
		t.Errorf("external_ref = %v, want nil", *order.CustomerExternalRef)
	}
	for i, item := range order.Items {
		if item.LineNo != i+1 {
			t.Errorf("items[%d].line_no = %d, want %d", i, item.LineNo, i+1)
		}
	}
}

func TestCreateOrderRequestValidate(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*payload.CreateOrderRequest)
		wantErrs int
	}{
		{name: "valid", mutate: func(*payload.CreateOrderRequest) {}},
		{name: "missing order id", mutate: func(r *payload.CreateOrderRequest) { r.OrderID = "" }, wantErrs: 1},
		{name: "missing tenant", mutate: func(r *payload.CreateOrderRequest) { r.TenantID = " " }, wantErrs: 1},
		{name: "missing confirmed at", mutate: func(r *payload.CreateOrderRequest) { r.ConfirmedAt = time.Time{} }, wantErrs: 1},
		{name: "missing confirming user", mutate: func(r *payload.CreateOrderRequest) { r.ConfirmedByUserID = "" }, wantErrs: 1},
		{name: "unsupported currency", mutate: func(r *payload.CreateOrderRequest) { r.Currency = "XYZ" }, wantErrs: 1},
		{name: "no items", mutate: func(r *payload.CreateOrderRequest) { r.Items = nil }, wantErrs: 1},
		{name: "blank sku", mutate: func(r *payload.CreateOrderRequest) { r.Items[0].SKU = "" }, wantErrs: 1},
		{name: "zero qty", mutate: func(r *payload.CreateOrderRequest) { r.Items[0].Qty = 0 }, wantErrs: 1},
		{name: "negative price", mutate: func(r *payload.CreateOrderRequest) { r.Items[0].UnitPrice = -1 }, wantErrs: 1},
		{name: "discount over 100", mutate: func(r *payload.CreateOrderRequest) { r.Items[0].DiscountPercent = 10001 }, wantErrs: 1},
		{
			name: "customer with no identifier at all",
			mutate: func(r *payload.CreateOrderRequest) {
				r.Customer.Phone = ""
				r.Customer.ExternalRef = ""
			},
			wantErrs: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := decode(t, itemBody("18.5", "10", 1))
			c.mutate(&req)

			if errs := req.Validate(); len(errs) != c.wantErrs {
				t.Errorf("Validate() = %v, want %d errors", errs, c.wantErrs)
			}
		})
	}
}
