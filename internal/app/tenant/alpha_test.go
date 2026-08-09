package tenant_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/money"
)

const alphaBaseURL = "http://erp-a.test"

func alphaOrder(items ...model.OrderItem) model.Order {
	ref := "PARTNER-42"
	return model.Order{
		OrderID:             "ord_1",
		TenantID:            pkg.TenantAlpha,
		Status:              pkg.OrderStatusConfirmed,
		Currency:            "MYR",
		CustomerExternalRef: &ref,
		ConfirmedAt:         time.Now().UTC(),
		Items:               items,
	}
}

func item(sku string, qty int64, price money.Amount, bps int64) model.OrderItem {
	return model.OrderItem{SKU: sku, Qty: qty, UnitPriceCents: price, DiscountBPS: bps}
}

// respond builds the response shape Interpret reads, including the SKUs the
// dispatcher recorded on the request.
func respond(status int, body string, skus ...string) erp.Response {
	return erp.Response{
		Request:    erp.Request{SKUs: skus},
		StatusCode: status,
		Body:       json.RawMessage(body),
	}
}

func TestAlphaPreValidate(t *testing.T) {
	ref, phone, blank := "PARTNER-42", "+60 12 345 6789", "   "

	cases := []struct {
		name     string
		order    model.Order
		wantCode string
	}{
		{
			name:  "external ref present",
			order: model.Order{Items: []model.OrderItem{item("SKU-1", 1, 1000, 0)}, CustomerExternalRef: &ref},
		},
		{
			name:  "phone stands in for a missing ref",
			order: model.Order{Items: []model.OrderItem{item("SKU-1", 1, 1000, 0)}, CustomerPhone: &phone},
		},
		{
			name:     "no line items",
			order:    model.Order{CustomerExternalRef: &ref},
			wantCode: pkg.CodeValidationError,
		},
		{
			name:     "no customer identity at all",
			order:    model.Order{Items: []model.OrderItem{item("SKU-1", 1, 1000, 0)}},
			wantCode: pkg.CodeValidationError,
		},
		{
			name:     "blank ref and no phone",
			order:    model.Order{Items: []model.OrderItem{item("SKU-1", 1, 1000, 0)}, CustomerExternalRef: &blank},
			wantCode: pkg.CodeValidationError,
		},
	}

	alpha := tenant.NewAlpha(alphaBaseURL)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			appErr := alpha.PreValidate(c.order, time.Now())

			if c.wantCode == "" {
				if appErr != nil {
					t.Fatalf("PreValidate: unexpected error %v", appErr)
				}
				return
			}
			if appErr == nil {
				t.Fatalf("PreValidate: want error %s, got nil", c.wantCode)
			}
			if appErr.Code != c.wantCode {
				t.Errorf("code = %s, want %s", appErr.Code, c.wantCode)
			}
		})
	}
}

// ERP A rejects a third line item in one call, so Build must chunk.
func TestAlphaBuildChunking(t *testing.T) {
	cases := []struct {
		name        string
		items       []model.OrderItem
		wantChunks  int
		wantSKUs    [][]string
		wantLastLen int
	}{
		{
			name:        "single line fits one chunk",
			items:       []model.OrderItem{item("A", 1, 1000, 0)},
			wantChunks:  1,
			wantSKUs:    [][]string{{"A"}},
			wantLastLen: 1,
		},
		{
			name:        "exactly at the limit",
			items:       []model.OrderItem{item("A", 1, 1000, 0), item("B", 1, 1000, 0)},
			wantChunks:  1,
			wantSKUs:    [][]string{{"A", "B"}},
			wantLastLen: 2,
		},
		{
			name:        "one over the limit spills into a second chunk",
			items:       []model.OrderItem{item("A", 1, 1000, 0), item("B", 1, 1000, 0), item("C", 1, 1000, 0)},
			wantChunks:  2,
			wantSKUs:    [][]string{{"A", "B"}, {"C"}},
			wantLastLen: 1,
		},
		{
			name: "five lines become three chunks",
			items: []model.OrderItem{
				item("A", 1, 1000, 0), item("B", 1, 1000, 0), item("C", 1, 1000, 0),
				item("D", 1, 1000, 0), item("E", 1, 1000, 0),
			},
			wantChunks:  3,
			wantSKUs:    [][]string{{"A", "B"}, {"C", "D"}, {"E"}},
			wantLastLen: 1,
		},
	}

	alpha := tenant.NewAlpha(alphaBaseURL)
	wantKey := model.IdempotencyKey("ord_1", pkg.TenantAlpha)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requests, appErr := alpha.Build(alphaOrder(c.items...))
			if appErr != nil {
				t.Fatalf("Build: %v", appErr)
			}

			if len(requests) != c.wantChunks {
				t.Fatalf("requests = %d, want %d", len(requests), c.wantChunks)
			}

			for i, req := range requests {
				if req.Method != http.MethodPost {
					t.Errorf("chunk %d method = %s, want POST", i, req.Method)
				}
				if req.ChunkIndex != i {
					t.Errorf("chunk %d ChunkIndex = %d", i, req.ChunkIndex)
				}
				if req.Headers[pkg.HeaderIdempotencyKey] != wantKey {
					t.Errorf("chunk %d idempotency key = %q, want %q", i, req.Headers[pkg.HeaderIdempotencyKey], wantKey)
				}
				// Same key on every chunk, so the index is what stops ERP A collapsing them.
				if got := req.Headers[pkg.HeaderChunkIndex]; got != strconv.Itoa(i) {
					t.Errorf("chunk %d index header = %q, want %q", i, got, strconv.Itoa(i))
				}
				if !slices.Equal(req.SKUs, c.wantSKUs[i]) {
					t.Errorf("chunk %d SKUs = %v, want %v", i, req.SKUs, c.wantSKUs[i])
				}

				body, ok := req.Body.(tenant.AlphaPayload)
				if !ok {
					t.Fatalf("chunk %d body type = %T, want AlphaPayload", i, req.Body)
				}
				if body.ChunkTotal != c.wantChunks {
					t.Errorf("chunk %d ChunkTotal = %d, want %d", i, body.ChunkTotal, c.wantChunks)
				}
				if len(body.LineItems) > tenant.AlphaMaxLinesPerRequest {
					t.Errorf("chunk %d carries %d lines, over the limit of %d", i, len(body.LineItems), tenant.AlphaMaxLinesPerRequest)
				}
			}

			last := requests[len(requests)-1].Body.(tenant.AlphaPayload)
			if len(last.LineItems) != c.wantLastLen {
				t.Errorf("last chunk lines = %d, want %d", len(last.LineItems), c.wantLastLen)
			}
		})
	}
}

func TestAlphaBuildPayload(t *testing.T) {
	ref, phone := "PARTNER-42", "+60 12 345 6789"

	cases := []struct {
		name           string
		order          model.Order
		wantCustomerID string
		wantLineTotal  money.Amount
	}{
		{
			name:           "external ref wins",
			order:          model.Order{OrderID: "ord_1", Currency: "MYR", CustomerExternalRef: &ref, CustomerPhone: &phone, Items: []model.OrderItem{item("SKU-1", 10, 1850, 1000)}},
			wantCustomerID: "PARTNER-42",
			wantLineTotal:  16650,
		},
		{
			name:           "phone is stripped of plus and spaces",
			order:          model.Order{OrderID: "ord_1", Currency: "MYR", CustomerPhone: &phone, Items: []model.OrderItem{item("SKU-1", 1, 1000, 0)}},
			wantCustomerID: "60123456789",
			wantLineTotal:  1000,
		},
	}

	alpha := tenant.NewAlpha(alphaBaseURL)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requests, appErr := alpha.Build(c.order)
			if appErr != nil {
				t.Fatalf("Build: %v", appErr)
			}

			body := requests[0].Body.(tenant.AlphaPayload)
			if body.CustomerID != c.wantCustomerID {
				t.Errorf("Customer_ID = %q, want %q", body.CustomerID, c.wantCustomerID)
			}
			if got := body.LineItems[0].LineTotalCents; got != c.wantLineTotal {
				t.Errorf("Line_Total_Cents = %d (%s), want %d", got, got, c.wantLineTotal)
			}

			// The wire format is integer cents; a decimal string here would be rejected.
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				LineItems []struct {
					LineTotalCents json.Number `json:"Line_Total_Cents"`
				} `json:"Line_Items"`
			}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.LineItems[0].LineTotalCents.String() != strconv.FormatInt(int64(c.wantLineTotal), 10) {
				t.Errorf("serialised Line_Total_Cents = %s, want %d", decoded.LineItems[0].LineTotalCents, c.wantLineTotal)
			}
		})
	}
}

func TestAlphaInterpret(t *testing.T) {
	cases := []struct {
		name         string
		responses    []erp.Response
		wantStatus   string
		wantMsg      string
		wantErrCode  string
		wantRejected []string
	}{
		{
			name:       "all chunks accepted",
			responses:  []erp.Response{respond(http.StatusCreated, `{"erp_order_id":"SAP-1"}`, "A", "B")},
			wantStatus: pkg.StatusSynced,
			wantMsg:    pkg.MsgSyncSuccess,
		},
		{
			// The first chunk is already in ERP A, so this is partial, not failed.
			name: "later chunk rejected",
			responses: []erp.Response{
				respond(http.StatusCreated, `{}`, "A", "B"),
				respond(http.StatusBadRequest, `{"error":"SKU_NOT_FOUND"}`, "C"),
			},
			wantStatus:   pkg.StatusPartialSuccess,
			wantMsg:      pkg.MsgSyncPartial,
			wantErrCode:  pkg.CodeERPRejected,
			wantRejected: []string{"C"},
		},
		{
			name:         "only chunk rejected",
			responses:    []erp.Response{respond(http.StatusBadRequest, `{"message":"ORDER_EXPIRED"}`, "A")},
			wantStatus:   pkg.StatusFailed,
			wantMsg:      pkg.MsgSyncFailed,
			wantErrCode:  pkg.CodeERPRejected,
			wantRejected: []string{"A"},
		},
		{
			name: "transport failure stops the order short",
			responses: []erp.Response{
				{Request: erp.Request{SKUs: []string{"A"}}, TransportErr: errors.New("connection refused")},
			},
			wantStatus:   pkg.StatusFailed,
			wantMsg:      pkg.MsgSyncFailed,
			wantErrCode:  pkg.CodeERPRejected,
			wantRejected: []string{"A"},
		},
		{
			// 207 is not a plain success, so it must not be reported as SYNCED.
			name:         "multi status is not a success",
			responses:    []erp.Response{respond(http.StatusMultiStatus, `{}`, "A")},
			wantStatus:   pkg.StatusFailed,
			wantMsg:      pkg.MsgSyncFailed,
			wantErrCode:  pkg.CodeERPRejected,
			wantRejected: []string{"A"},
		},
	}

	alpha := tenant.NewAlpha(alphaBaseURL)
	order := alphaOrder(item("A", 1, 1000, 0))

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := alpha.Interpret(order, c.responses)

			if out.Status != c.wantStatus {
				t.Errorf("status = %s, want %s", out.Status, c.wantStatus)
			}
			if out.MessageCode != c.wantMsg {
				t.Errorf("message code = %s, want %s", out.MessageCode, c.wantMsg)
			}
			if out.ErrorCode != c.wantErrCode {
				t.Errorf("error code = %q, want %q", out.ErrorCode, c.wantErrCode)
			}

			rejected := model.SyncAttempt{Lines: out.Lines}.RejectedSKUs()
			if !slices.Equal(rejected, c.wantRejected) {
				t.Errorf("rejected SKUs = %v, want %v", rejected, c.wantRejected)
			}
			for _, line := range out.Lines {
				if !line.Accepted && (line.Reason == nil || *line.Reason == "") {
					t.Errorf("rejected SKU %s carries no reason", line.SKU)
				}
			}
		})
	}
}
