package mockerp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"zolo-test-integration/internal/mockerp"
)

const (
	alphaPath = "/api/v1/sap-adapter/orders"
	betaPath  = "/api/v2/odoo-adapter/sales-order"
)

func post(t *testing.T, api http.Handler, path, body, idempotencyKey string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	var decoded map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}

	return rec.Code, decoded
}

func alphaBody(chunkIndex int, skus ...string) string {
	lines := make([]string, 0, len(skus))
	for _, sku := range skus {
		lines = append(lines, `{"SKU":"`+sku+`","Qty":1,"Unit_Price_Cents":1000,"Line_Total_Cents":1000}`)
	}
	return `{"Order_ID":"ord_1","Customer_ID":"CUST-882","Currency":"MYR","Chunk_Index":` +
		strconv.Itoa(chunkIndex) + `,"Line_Items":[` + strings.Join(lines, ",") + `]}`
}

func betaBody(partnerID int, skus ...string) string {
	lines := make([]string, 0, len(skus))
	for _, sku := range skus {
		lines = append(lines, `{"product_code":"`+sku+`","product_uom_qty":1,"price_subtotal":1000}`)
	}
	return `{"order_ref":"ord_1","partner_id":` + strconv.Itoa(partnerID) +
		`,"order_lines":[` + strings.Join(lines, ",") + `]}`
}

func betaBodyConfirmedAt(confirmedAt time.Time, skus ...string) string {
	return strings.Replace(
		betaBody(882, skus...),
		`"order_ref":"ord_1"`,
		`"order_ref":"ord_1","confirmed_at":"`+confirmedAt.UTC().Format(time.RFC3339)+`"`,
		1,
	)
}

// ERP A's rate limit above two line items is the behaviour that forces the client to chunk.
func TestAlphaRateLimit(t *testing.T) {
	cases := []struct {
		name       string
		skus       []string
		wantStatus int
		wantError  string
	}{
		{name: "one line", skus: []string{"SKU-1"}, wantStatus: http.StatusCreated},
		{name: "at the limit", skus: []string{"SKU-1", "SKU-2"}, wantStatus: http.StatusCreated},
		{
			name:       "one over the limit",
			skus:       []string{"SKU-1", "SKU-2", "SKU-3"},
			wantStatus: http.StatusTooManyRequests,
			wantError:  "RATE_LIMITED",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := mockerp.NewServer(nil).Handler()

			status, body := post(t, api, alphaPath, alphaBody(0, c.skus...), "key-1")

			if status != c.wantStatus {
				t.Errorf("status = %d, want %d", status, c.wantStatus)
			}
			if c.wantError != "" && body["error"] != c.wantError {
				t.Errorf("error = %v, want %s", body["error"], c.wantError)
			}
		})
	}
}

func TestAlphaValidation(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{name: "valid order", body: alphaBody(0, "SKU-1"), wantStatus: http.StatusCreated},
		{
			name:       "missing customer",
			body:       `{"Order_ID":"ord_1","Line_Items":[{"SKU":"SKU-1"}]}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "MISSING_CUSTOMER",
		},
		{
			name:       "malformed payload",
			body:       `{"Order_ID":`,
			wantStatus: http.StatusBadRequest,
			wantError:  "MALFORMED_PAYLOAD",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := mockerp.NewServer(nil).Handler()

			status, body := post(t, api, alphaPath, c.body, "key-1")

			if status != c.wantStatus {
				t.Errorf("status = %d, want %d", status, c.wantStatus)
			}
			if c.wantError != "" && body["error"] != c.wantError {
				t.Errorf("error = %v, want %s", body["error"], c.wantError)
			}
		})
	}
}

// A chunked order sends the same order-level key more than once, so only a repeat of the
// same chunk counts as a duplicate.
func TestAlphaIdempotency(t *testing.T) {
	cases := []struct {
		name          string
		firstChunk    int
		secondChunk   int
		secondKey     string
		wantDuplicate bool
	}{
		{name: "same key and chunk replays", secondKey: "key-1", wantDuplicate: true},
		{name: "same key, next chunk is not a duplicate", secondChunk: 1, secondKey: "key-1"},
		{name: "different key is a different order", secondKey: "key-2"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := mockerp.NewServer(nil)
			api := srv.Handler()

			if status, _ := post(t, api, alphaPath, alphaBody(c.firstChunk, "SKU-1"), "key-1"); status != http.StatusCreated {
				t.Fatalf("first post status = %d, want 201", status)
			}

			status, body := post(t, api, alphaPath, alphaBody(c.secondChunk, "SKU-1"), c.secondKey)

			if status != http.StatusCreated {
				t.Fatalf("second post status = %d, want 201", status)
			}
			if got, _ := body["duplicate"].(bool); got != c.wantDuplicate {
				t.Errorf("duplicate = %v, want %v", got, c.wantDuplicate)
			}
			if got := srv.Calls(alphaPath); got != 2 {
				t.Errorf("recorded calls = %d, want 2", got)
			}
		})
	}
}

// ERP B's 207 is the case the integration service must not flatten into a total failure.
func TestBetaPartialAcceptance(t *testing.T) {
	cases := []struct {
		name         string
		skus         []string
		wantStatus   int
		wantRejected int
	}{
		{name: "all in stock", skus: []string{"SKU-1", "SKU-2"}, wantStatus: http.StatusOK},
		{
			name:         "one out of stock",
			skus:         []string{"SKU-1", "SKU-OUTOFSTOCK"},
			wantStatus:   http.StatusMultiStatus,
			wantRejected: 1,
		},
		{
			name:         "all out of stock",
			skus:         []string{"SKU-OUTOFSTOCK", "OTHER-OUTOFSTOCK"},
			wantStatus:   http.StatusMultiStatus,
			wantRejected: 2,
		},
		{
			name:         "marker match is case insensitive",
			skus:         []string{"sku-outofstock"},
			wantStatus:   http.StatusMultiStatus,
			wantRejected: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := mockerp.NewServer(nil).Handler()

			status, body := post(t, api, betaPath, betaBody(882, c.skus...), "key-1")

			if status != c.wantStatus {
				t.Errorf("status = %d, want %d", status, c.wantStatus)
			}

			lines, _ := body["line_results"].([]any)
			if len(lines) != len(c.skus) {
				t.Fatalf("line results = %d, want %d", len(lines), len(c.skus))
			}

			rejected := 0
			for _, line := range lines {
				entry := line.(map[string]any)
				if entry["status"] == "failed" {
					rejected++
					if entry["reason"] == "" {
						t.Errorf("rejected %v carries no reason", entry["product_code"])
					}
				}
			}
			if rejected != c.wantRejected {
				t.Errorf("rejected lines = %d, want %d", rejected, c.wantRejected)
			}
		})
	}
}

func TestBetaValidation(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{name: "valid order", body: betaBody(882, "SKU-1"), wantStatus: http.StatusOK},
		{name: "missing partner", body: betaBody(0, "SKU-1"), wantStatus: http.StatusBadRequest, wantError: "INVALID_PARTNER"},
		{
			name:       "no lines",
			body:       `{"order_ref":"ord_1","partner_id":882,"order_lines":[]}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "EMPTY_ORDER",
		},
		{
			name:       "malformed payload",
			body:       `{"order_ref":`,
			wantStatus: http.StatusBadRequest,
			wantError:  "MALFORMED_PAYLOAD",
		},
		{
			// The integration catches expiry in pre-validation, but ERP B enforces the
			// same window, so an order that crosses it in flight is rejected here.
			name:       "confirmed more than 24 hours ago",
			body:       betaBodyConfirmedAt(time.Now().Add(-25*time.Hour), "SKU-1"),
			wantStatus: http.StatusBadRequest,
			wantError:  "ORDER_EXPIRED",
		},
		{
			name:       "confirmed inside the window",
			body:       betaBodyConfirmedAt(time.Now().Add(-23*time.Hour), "SKU-1"),
			wantStatus: http.StatusOK,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := mockerp.NewServer(nil).Handler()

			status, body := post(t, api, betaPath, c.body, "key-1")

			if status != c.wantStatus {
				t.Errorf("status = %d, want %d", status, c.wantStatus)
			}
			if c.wantError != "" && body["error"] != c.wantError {
				t.Errorf("error = %v, want %s", body["error"], c.wantError)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	api := mockerp.NewServer(nil).Handler()

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
