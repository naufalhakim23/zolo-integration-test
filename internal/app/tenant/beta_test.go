package tenant_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/money"
)

const betaBaseURL = "http://erp-b.test"

var betaNow = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func betaOrder(ref string, confirmedAt time.Time, items ...model.OrderItem) model.Order {
	return model.Order{
		OrderID:             "ord_1",
		TenantID:            pkg.TenantBeta,
		Status:              pkg.OrderStatusConfirmed,
		Currency:            "MYR",
		CustomerExternalRef: &ref,
		ConfirmedAt:         confirmedAt,
		Items:               items,
	}
}

func TestParsePartnerID(t *testing.T) {
	cases := []struct {
		name    string
		ref     string
		want    int64
		wantErr bool
	}{
		{name: "standard reference", ref: "CUST-882", want: 882},
		{name: "padded", ref: "  CUST-882  ", want: 882},
		{name: "lowercase prefix", ref: "cust-1", want: 1},
		{name: "long prefix", ref: "PARTNER-4242", want: 4242},
		{name: "empty", ref: "", wantErr: true},
		{name: "blank", ref: "   ", wantErr: true},
		{name: "no number", ref: "CUST-", wantErr: true},
		{name: "no prefix", ref: "882", wantErr: true},
		{name: "digits in prefix", ref: "CUST2-882", wantErr: true},
		{name: "trailing text", ref: "CUST-882-X", wantErr: true},
		{name: "zero is not a partner", ref: "CUST-0", wantErr: true},
		{name: "overflows int64", ref: "CUST-99999999999999999999", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tenant.ParsePartnerID(c.ref)

			if c.wantErr {
				if err == nil {
					t.Fatalf("ParsePartnerID(%q) = %d, want error", c.ref, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePartnerID(%q): %v", c.ref, err)
			}
			if got != c.want {
				t.Errorf("ParsePartnerID(%q) = %d, want %d", c.ref, got, c.want)
			}
		})
	}
}

func TestBetaPreValidate(t *testing.T) {
	line := item("SKU-1", 1, 1000, 0)

	cases := []struct {
		name     string
		order    model.Order
		wantCode string
	}{
		{name: "fresh order", order: betaOrder("CUST-882", betaNow, line)},
		{
			name:  "just inside the expiry window",
			order: betaOrder("CUST-882", betaNow.Add(-24*time.Hour+time.Second), line),
		},
		{
			// The boundary is inclusive: exactly 24h old is still syncable.
			name:  "exactly at the boundary",
			order: betaOrder("CUST-882", betaNow.Add(-24*time.Hour), line),
		},
		{
			name:     "one second past expiry",
			order:    betaOrder("CUST-882", betaNow.Add(-24*time.Hour-time.Second), line),
			wantCode: pkg.CodeOrderExpired,
		},
		{
			name:     "no line items",
			order:    betaOrder("CUST-882", betaNow),
			wantCode: pkg.CodeValidationError,
		},
		{
			name:     "unmappable partner ref",
			order:    betaOrder("WALK-IN", betaNow, line),
			wantCode: pkg.CodeValidationError,
		},
		{
			// Expiry is checked before the ref, so a stale order reports the actionable reason.
			name:     "expired and unmappable reports expiry",
			order:    betaOrder("WALK-IN", betaNow.Add(-48*time.Hour), line),
			wantCode: pkg.CodeOrderExpired,
		},
	}

	beta := tenant.NewBeta(betaBaseURL)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			appErr := beta.PreValidate(c.order, betaNow)

			if c.wantCode == "" {
				if appErr != nil {
					t.Fatalf("PreValidate: unexpected error %v", appErr)
				}
				return
			}
			if appErr == nil {
				t.Fatalf("PreValidate: want %s, got nil", c.wantCode)
			}
			if appErr.Code != c.wantCode {
				t.Errorf("code = %s, want %s", appErr.Code, c.wantCode)
			}
		})
	}
}

// ERP B takes the whole order in one call, with SST applied once on the header.
func TestBetaBuild(t *testing.T) {
	cases := []struct {
		name          string
		items         []model.OrderItem
		wantUntaxed   money.Amount
		wantTax       money.Amount
		wantTotal     money.Amount
		wantPartnerID int64
	}{
		{
			name:          "single discounted line",
			items:         []model.OrderItem{item("SKU-1", 10, 1850, 1000)},
			wantUntaxed:   16650,
			wantTax:       1332,
			wantTotal:     17982,
			wantPartnerID: 882,
		},
		{
			name:          "two lines",
			items:         []model.OrderItem{item("SKU-1", 10, 1850, 1000), item("SKU-2", 6, 1000, 0)},
			wantUntaxed:   22650,
			wantTax:       1812,
			wantTotal:     24462,
			wantPartnerID: 882,
		},
		{
			// Beta never chunks, so the per-request limit that constrains ERP A does not apply.
			name: "five lines still go in one request",
			items: []model.OrderItem{
				item("A", 1, 1000, 0), item("B", 1, 1000, 0), item("C", 1, 1000, 0),
				item("D", 1, 1000, 0), item("E", 1, 1000, 0),
			},
			wantUntaxed:   5000,
			wantTax:       400,
			wantTotal:     5400,
			wantPartnerID: 882,
		},
	}

	beta := tenant.NewBeta(betaBaseURL)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			order := betaOrder("CUST-882", betaNow, c.items...)

			requests, appErr := beta.Build(order)
			if appErr != nil {
				t.Fatalf("Build: %v", appErr)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}

			req := requests[0]
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", req.Method)
			}
			if got, want := req.Headers[pkg.HeaderIdempotencyKey], model.IdempotencyKey("ord_1", pkg.TenantBeta); got != want {
				t.Errorf("idempotency key = %q, want %q", got, want)
			}
			if len(req.SKUs) != len(c.items) {
				t.Errorf("SKUs = %d, want %d", len(req.SKUs), len(c.items))
			}

			body, ok := req.Body.(tenant.BetaPayload)
			if !ok {
				t.Fatalf("body type = %T, want BetaPayload", req.Body)
			}
			if body.PartnerID != c.wantPartnerID {
				t.Errorf("partner_id = %d, want %d", body.PartnerID, c.wantPartnerID)
			}
			if body.AmountUntaxed != c.wantUntaxed {
				t.Errorf("amount_untaxed = %d (%s), want %d", body.AmountUntaxed, body.AmountUntaxed, c.wantUntaxed)
			}
			if body.AmountTax != c.wantTax {
				t.Errorf("amount_tax = %d (%s), want %d", body.AmountTax, body.AmountTax, c.wantTax)
			}
			if body.AmountTotal != c.wantTotal {
				t.Errorf("amount_total = %d (%s), want %d", body.AmountTotal, body.AmountTotal, c.wantTotal)
			}
			if body.AmountUntaxed+body.AmountTax != body.AmountTotal {
				t.Errorf("header totals do not add up: %d + %d != %d", body.AmountUntaxed, body.AmountTax, body.AmountTotal)
			}
			if body.TaxPercent != tenant.BetaSSTPercent {
				t.Errorf("tax_percent = %d, want %d", body.TaxPercent, tenant.BetaSSTPercent)
			}
		})
	}
}

func TestBetaBuildRejectsUnmappableRef(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{name: "walk-in customer", ref: "WALK-IN"},
		{name: "missing ref", ref: ""},
	}

	beta := tenant.NewBeta(betaBaseURL)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, appErr := beta.Build(betaOrder(c.ref, betaNow, item("SKU-1", 1, 1000, 0)))
			if appErr == nil {
				t.Fatal("Build: want validation error, got nil")
			}
			if appErr.Code != pkg.CodeValidationError {
				t.Errorf("code = %s, want %s", appErr.Code, pkg.CodeValidationError)
			}
		})
	}
}

func TestBetaInterpret(t *testing.T) {
	items := []model.OrderItem{item("SKU-1", 1, 1000, 0), item("SKU-2", 1, 1000, 0)}
	order := betaOrder("CUST-882", betaNow, items...)

	cases := []struct {
		name         string
		responses    []erp.Response
		wantStatus   string
		wantErrCode  string
		wantMsg      string
		wantRejected []string
	}{
		{
			name:       "accepted",
			responses:  []erp.Response{respond(http.StatusCreated, `{"id":501}`, "SKU-1", "SKU-2")},
			wantStatus: pkg.StatusSynced,
			wantMsg:    pkg.MsgSyncSuccess,
		},
		{
			name: "207 with one line rejected",
			responses: []erp.Response{respond(http.StatusMultiStatus,
				`{"line_results":[{"product_code":"SKU-1","status":"success"},{"product_code":"SKU-2","status":"rejected","reason":"OUT_OF_STOCK"}]}`,
				"SKU-1", "SKU-2")},
			wantStatus:   pkg.StatusPartialSuccess,
			wantErrCode:  pkg.CodeERPRejected,
			wantMsg:      pkg.MsgSyncPartial,
			wantRejected: []string{"SKU-2"},
		},
		{
			// A 207 where nothing landed is a failure wearing a partial status code.
			name: "207 with every line rejected",
			responses: []erp.Response{respond(http.StatusMultiStatus,
				`{"line_results":[{"product_code":"SKU-1","status":"rejected","reason":"OUT_OF_STOCK"},{"product_code":"SKU-2","status":"rejected","reason":"OUT_OF_STOCK"}]}`,
				"SKU-1", "SKU-2")},
			wantStatus:   pkg.StatusFailed,
			wantErrCode:  pkg.CodeERPRejected,
			wantMsg:      pkg.MsgSyncFailed,
			wantRejected: []string{"SKU-1", "SKU-2"},
		},
		{
			// A line the ERP never mentions counts as rejected, not silently accepted.
			name: "207 omitting a line",
			responses: []erp.Response{respond(http.StatusMultiStatus,
				`{"line_results":[{"product_code":"SKU-1","status":"success"}]}`,
				"SKU-1", "SKU-2")},
			wantStatus:   pkg.StatusPartialSuccess,
			wantErrCode:  pkg.CodeERPRejected,
			wantMsg:      pkg.MsgSyncPartial,
			wantRejected: []string{"SKU-2"},
		},
		{
			// Expiry crossed in flight is mapped, not flattened into a generic rejection.
			name:         "server-side expiry",
			responses:    []erp.Response{respond(http.StatusBadRequest, `{"error":"ORDER_EXPIRED"}`, "SKU-1", "SKU-2")},
			wantStatus:   pkg.StatusFailed,
			wantErrCode:  pkg.CodeOrderExpired,
			wantMsg:      pkg.MsgOrderExpired,
			wantRejected: []string{"SKU-1", "SKU-2"},
		},
		{
			name:         "generic rejection",
			responses:    []erp.Response{respond(http.StatusBadRequest, `{"error":"INVALID_PARTNER"}`, "SKU-1", "SKU-2")},
			wantStatus:   pkg.StatusFailed,
			wantErrCode:  pkg.CodeERPRejected,
			wantMsg:      pkg.MsgSyncFailed,
			wantRejected: []string{"SKU-1", "SKU-2"},
		},
		{
			name:         "transport failure",
			responses:    []erp.Response{{Request: erp.Request{SKUs: []string{"SKU-1"}}, TransportErr: errors.New("connection refused")}},
			wantStatus:   pkg.StatusFailed,
			wantErrCode:  pkg.CodeERPUnavailable,
			wantMsg:      pkg.MsgERPUnavailable,
			wantRejected: []string{"SKU-1", "SKU-2"},
		},
		{
			name:         "no response at all",
			wantStatus:   pkg.StatusFailed,
			wantErrCode:  pkg.CodeERPUnavailable,
			wantMsg:      pkg.MsgSyncFailed,
			wantRejected: []string{"SKU-1", "SKU-2"},
		},
	}

	beta := tenant.NewBeta(betaBaseURL)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := beta.Interpret(order, c.responses)

			if out.Status != c.wantStatus {
				t.Errorf("status = %s, want %s", out.Status, c.wantStatus)
			}
			if out.ErrorCode != c.wantErrCode {
				t.Errorf("error code = %q, want %q", out.ErrorCode, c.wantErrCode)
			}
			if out.MessageCode != c.wantMsg {
				t.Errorf("message code = %s, want %s", out.MessageCode, c.wantMsg)
			}

			rejected := model.SyncAttempt{Lines: out.Lines}.RejectedSKUs()
			if !slices.Equal(rejected, c.wantRejected) {
				t.Errorf("rejected SKUs = %v, want %v", rejected, c.wantRejected)
			}
			// Every order line is accounted for, whatever the outcome.
			if len(out.Lines) != len(items) {
				t.Errorf("lines = %d, want %d", len(out.Lines), len(items))
			}
			for _, line := range out.Lines {
				if !line.Accepted && (line.Reason == nil || *line.Reason == "") {
					t.Errorf("rejected SKU %s carries no reason", line.SKU)
				}
			}
		})
	}
}

func TestBetaPayloadWireFormat(t *testing.T) {
	requests, appErr := tenant.NewBeta(betaBaseURL).
		Build(betaOrder("CUST-882", betaNow, item("SKU-1", 10, 1850, 1000)))
	if appErr != nil {
		t.Fatalf("Build: %v", appErr)
	}

	raw, err := json.Marshal(requests[0].Body)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		field string
		want  json.Number
	}{
		{name: "partner id", field: "partner_id", want: "882"},
		{name: "untaxed total in cents", field: "amount_untaxed", want: "16650"},
		{name: "tax in cents", field: "amount_tax", want: "1332"},
		{name: "grand total in cents", field: "amount_total", want: "17982"},
		{name: "tax percent", field: "tax_percent", want: "8"},
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := decoded[c.field]
			if !ok {
				t.Fatalf("%s missing from payload %s", c.field, raw)
			}
			if string(got) != string(c.want) {
				t.Errorf("%s = %s, want %s", c.field, got, c.want)
			}
		})
	}

	// ERP B enforces its own 24-hour window, so dropping this field would leave the
	// server-side ORDER_EXPIRED rejection unreachable.
	t.Run("confirmed at", func(t *testing.T) {
		got, ok := decoded["confirmed_at"]
		if !ok {
			t.Fatalf("confirmed_at missing from payload %s", raw)
		}

		var sent time.Time
		if err := json.Unmarshal(got, &sent); err != nil {
			t.Fatalf("confirmed_at is not a timestamp: %v", err)
		}
		if !sent.Equal(betaNow) {
			t.Errorf("confirmed_at = %s, want %s", sent, betaNow)
		}
	})
}

func TestDefaultRegistry(t *testing.T) {
	cfg := config.ERP{AlphaBaseURL: alphaBaseURL, BetaBaseURL: betaBaseURL}
	registry := tenant.DefaultRegistry(cfg)

	cases := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{name: "alpha is wired", tenantID: pkg.TenantAlpha},
		{name: "beta is wired", tenantID: pkg.TenantBeta},
		{name: "unknown tenant", tenantID: "tenant_gamma", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, appErr := registry.Get(c.tenantID)

			if c.wantErr {
				if appErr == nil {
					t.Fatalf("Get(%q) = %v, want error", c.tenantID, m)
				}
				return
			}
			if appErr != nil {
				t.Fatalf("Get(%q): %v", c.tenantID, appErr)
			}
			if m.TenantID() != c.tenantID {
				t.Errorf("mapper = %q, want %q", m.TenantID(), c.tenantID)
			}
		})
	}
}
