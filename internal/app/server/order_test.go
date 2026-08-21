package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/pkg"
)

// specPayload is the confirmed-order body from the exercise brief, verbatim. It is the
// one case where the service must turn major-unit decimals into cents itself.
const specPayload = `{
  "order_id": "ord_998123_new",
  "tenant_id": "tenant_alpha",
  "confirmed_at": "2026-07-22T08:00:00Z",
  "confirmed_by_user_id": "usr_4410",
  "currency": "MYR",
  "customer": {
    "phone": "+60123456789",
    "external_ref": "CUST-882"
  },
  "items": [
    {
      "sku": "SKU-MILO-1KG",
      "qty": 10,
      "unit_price": 18.5,
      "discount_percent": 10
    },
    {
      "sku": "SKU-NESTUM-500G",
      "qty": 5,
      "unit_price": 12.0,
      "discount_percent": 0
    }
  ]
}`

func decodeCreated(t *testing.T, data any) payload.CreatedOrder {
	t.Helper()

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var created payload.CreatedOrder
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}

	return created
}

func decodeErrorCode(t *testing.T, data any) string {
	t.Helper()

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var detail payload.ErrorDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("decode error detail: %v", err)
	}

	return detail.Code
}

// The brief's payload has to survive ingestion and reach the ERP as integer cents:
// 18.50 x 0.9 x 10 = 16650, plus 12.00 x 5 = 6000, so 22650 with no drift.
func TestCreateOrderFromSpecPayloadThenSync(t *testing.T) {
	api, mock := newAPI(t)

	code, body := request(t, api, http.MethodPost, "/api/v1/orders", specPayload, "")
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %+v", code, body)
	}

	created := decodeCreated(t, body.Data)
	if created.SubtotalCents != 22650 {
		t.Errorf("subtotal_cents = %d, want 22650", created.SubtotalCents)
	}
	if created.Subtotal != "226.50" {
		t.Errorf("subtotal = %q, want %q", created.Subtotal, "226.50")
	}
	if created.Status != pkg.OrderStatusConfirmed {
		t.Errorf("status = %q, want %q", created.Status, pkg.OrderStatusConfirmed)
	}
	if created.LineCount != 2 {
		t.Errorf("line_count = %d, want 2", created.LineCount)
	}

	code, body = request(t, api, http.MethodPost, "/api/v1/orders/ord_998123_new/sync", "", "")
	if code != http.StatusOK {
		t.Fatalf("sync = %d, want 200: %+v", code, body)
	}

	if result := decodeResult(t, body.Data); result.Status != pkg.StatusSynced {
		t.Errorf("status = %q, want %q", result.Status, pkg.StatusSynced)
	}

	// Two lines fit ERP A's limit, so a correct build is exactly one call.
	if calls := mock.Calls("/api/v1/sap-adapter/orders"); calls != 1 {
		t.Errorf("erp calls = %d, want 1", calls)
	}
}

// A re-post of the same order_id is a duplicate confirmation. Accepting it as an update
// would change what an already-completed sync attempt was built from.
func TestCreateOrderRejectsDuplicate(t *testing.T) {
	api, _ := newAPI(t)

	if code, body := request(t, api, http.MethodPost, "/api/v1/orders", specPayload, ""); code != http.StatusCreated {
		t.Fatalf("first create = %d, want 201: %+v", code, body)
	}

	code, body := request(t, api, http.MethodPost, "/api/v1/orders", specPayload, "")
	if code != http.StatusConflict {
		t.Fatalf("second create = %d, want 409: %+v", code, body)
	}
	if got := decodeErrorCode(t, body.Error); got != pkg.CodeOrderExists {
		t.Errorf("error code = %q, want %q", got, pkg.CodeOrderExists)
	}
}

func TestCreateOrderRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantHTTP int
		wantCode string
	}{
		{
			name:     "unknown tenant never reaches storage",
			body:     strings.Replace(specPayload, "tenant_alpha", "tenant_gamma", 1),
			wantHTTP: http.StatusUnprocessableEntity,
			wantCode: pkg.CodeUnknownTenant,
		},
		{
			name:     "unsupported currency",
			body:     strings.Replace(specPayload, `"currency": "MYR"`, `"currency": "XYZ"`, 1),
			wantHTTP: http.StatusUnprocessableEntity,
			wantCode: pkg.CodeValidationError,
		},
		{
			name:     "non-positive quantity",
			body:     strings.Replace(specPayload, `"qty": 10`, `"qty": 0`, 1),
			wantHTTP: http.StatusUnprocessableEntity,
			wantCode: pkg.CodeValidationError,
		},
		{
			name:     "discount above 100 percent",
			body:     strings.Replace(specPayload, `"discount_percent": 10`, `"discount_percent": 140`, 1),
			wantHTTP: http.StatusUnprocessableEntity,
			wantCode: pkg.CodeValidationError,
		},
		{
			name: "customer with neither phone nor external ref",
			body: strings.Replace(specPayload, `"phone": "+60123456789",
    "external_ref": "CUST-882"`, `"phone": "",
    "external_ref": ""`, 1),
			wantHTTP: http.StatusUnprocessableEntity,
			wantCode: pkg.CodeValidationError,
		},
		{
			name:     "malformed json",
			body:     `{"order_id":`,
			wantHTTP: http.StatusBadRequest,
			wantCode: pkg.CodeBadRequest,
		},
		{
			name:     "price is not a number",
			body:     strings.Replace(specPayload, `"unit_price": 18.5`, `"unit_price": "abc"`, 1),
			wantHTTP: http.StatusBadRequest,
			wantCode: pkg.CodeBadRequest,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, _ := newAPI(t)

			code, body := request(t, api, http.MethodPost, "/api/v1/orders", c.body, "")
			if code != c.wantHTTP {
				t.Fatalf("code = %d, want %d: %+v", code, c.wantHTTP, body)
			}
			if got := decodeErrorCode(t, body.Error); got != c.wantCode {
				t.Errorf("error code = %q, want %q", got, c.wantCode)
			}

			// A rejected confirmation must leave nothing behind to sync.
			if syncCode, _ := request(t, api, http.MethodPost, "/api/v1/orders/ord_998123_new/sync", "", ""); syncCode != http.StatusNotFound {
				t.Errorf("sync after rejected create = %d, want 404", syncCode)
			}
		})
	}
}
