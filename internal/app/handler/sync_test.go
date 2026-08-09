package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/app/repository"
	"zolo-test-integration/internal/app/server"
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/migrations"
	"zolo-test-integration/pkg/driver"
)

// newAPI serves the real router over the real stack, with only the ERP stubbed.
func newAPI(t *testing.T, erpStatus int, erpBody string, tenantID string, confirmedAt time.Time) http.Handler {
	t.Helper()

	erpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(erpStatus)
		_, _ = w.Write([]byte(erpBody))
	}))
	t.Cleanup(erpSrv.Close)

	db, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.Run(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO orders (order_id, tenant_id, status, confirmed_at, confirmed_by_user_id, currency, customer_external_ref)
		VALUES ('ord_1', ?, 'CONFIRMED', ?, 'user_1', 'MYR', 'CUST-882')`,
		tenantID, confirmedAt)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO order_items (order_id, line_no, sku, qty, unit_price_cents, discount_bps)
		VALUES ('ord_1', 1, 'SKU-1', 10, 1850, 1000), ('ord_1', 2, 'SKU-2', 6, 1000, 0)`)
	if err != nil {
		t.Fatalf("seed items: %v", err)
	}

	options := pkg.OptionsApplication{
		DB:        db,
		Logger:    slog.New(slog.DiscardHandler),
		Localizer: pkg.NewLocalizer(pkg.LangEN),
	}
	cfg := config.ERP{
		AlphaBaseURL: erpSrv.URL,
		BetaBaseURL:  erpSrv.URL,
		Timeout:      2 * time.Second,
		MaxAttempts:  1,
		BackoffBase:  time.Millisecond,
		BackoffMax:   time.Millisecond,
	}

	repo := &repository.Repository{
		Order: repository.InitiateOrderRepository(repository.RepositoryOption{OptionsApplication: options}),
		Sync:  repository.InitiateSyncRepository(repository.RepositoryOption{OptionsApplication: options}),
	}
	svc := &service.Service{
		Sync: service.InitiateSyncService(service.ServiceOption{
			OptionsApplication: options,
			Repository:         repo,
			Registry:           tenant.DefaultRegistry(cfg),
			ERPClient:          erp.NewClient(cfg, options.Logger),
		}),
	}

	return server.NewServer(options, svc).Handler()
}

func call(t *testing.T, api http.Handler, method, path, body, lang string) (int, payload.BaseResponse) {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	var decoded payload.BaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}

	return rec.Code, decoded
}

// The status line alone tells the dashboard what happened, so it never has to parse
// the body to decide whether to prompt the order taker.
func TestSyncOrderHTTPStatus(t *testing.T) {
	fresh := time.Now().UTC()

	cases := []struct {
		name        string
		tenantID    string
		erpStatus   int
		erpBody     string
		confirmedAt time.Time
		wantHTTP    int
		wantMessage string
	}{
		{
			name:        "accepted is 200",
			tenantID:    pkg.TenantBeta,
			erpStatus:   http.StatusCreated,
			erpBody:     `{"id":501}`,
			confirmedAt: fresh,
			wantHTTP:    http.StatusOK,
			wantMessage: "Order ord_1 was sent to the ERP successfully.",
		},
		{
			name:        "partial success is 207",
			tenantID:    pkg.TenantBeta,
			erpStatus:   http.StatusMultiStatus,
			erpBody:     `{"line_results":[{"product_code":"SKU-1","status":"success"},{"product_code":"SKU-2","status":"rejected","reason":"OUT_OF_STOCK"}]}`,
			confirmedAt: fresh,
			wantHTTP:    http.StatusMultiStatus,
			wantMessage: "Order ord_1 was partly accepted. These items were rejected: SKU-2. Please review them and sync again.",
		},
		{
			name:        "erp rejection is 502",
			tenantID:    pkg.TenantAlpha,
			erpStatus:   http.StatusBadRequest,
			erpBody:     `{"error":"SKU_NOT_FOUND"}`,
			confirmedAt: fresh,
			wantHTTP:    http.StatusBadGateway,
		},
		{
			// Pre-flight rejections never reach the ERP, so they answer 422, not 502.
			name:        "expired order is 422",
			tenantID:    pkg.TenantBeta,
			erpStatus:   http.StatusCreated,
			erpBody:     `{"id":501}`,
			confirmedAt: time.Now().UTC().Add(-48 * time.Hour),
			wantHTTP:    http.StatusUnprocessableEntity,
			wantMessage: "Order ord_1 was confirmed more than 24 hours ago and the ERP will no longer accept it. Please re-confirm the order.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := newAPI(t, c.erpStatus, c.erpBody, c.tenantID, c.confirmedAt)

			code, body := call(t, api, http.MethodPost, "/api/v1/orders/ord_1/sync", "", "")

			if code != c.wantHTTP {
				t.Errorf("http status = %d, want %d (body %+v)", code, c.wantHTTP, body)
			}
			if body.Status != c.wantHTTP {
				t.Errorf("body status = %d, want %d", body.Status, c.wantHTTP)
			}
			if c.wantMessage != "" && body.Message != c.wantMessage {
				t.Errorf("message = %q, want %q", body.Message, c.wantMessage)
			}
			if body.Message == "" {
				t.Error("response carries no human-readable message")
			}
		})
	}
}

// A raw message code or an SQL string must never reach the dashboard.
func TestErrorResponsesAreLocalized(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		lang     string
		wantHTTP int
		wantCode string
		want     string
	}{
		{
			name:     "missing order in english",
			path:     "/api/v1/orders/ord_missing/sync",
			lang:     pkg.LangEN,
			wantHTTP: http.StatusNotFound,
			wantCode: pkg.CodeNotFound,
			want:     "Order ord_missing was not found.",
		},
		{
			name:     "missing order in malay",
			path:     "/api/v1/orders/ord_missing/sync",
			lang:     "ms-MY",
			wantHTTP: http.StatusNotFound,
			wantCode: pkg.CodeNotFound,
			want:     "Pesanan ord_missing tidak dijumpai.",
		},
		{
			name:     "unsupported language falls back to english",
			path:     "/api/v1/orders/ord_missing/sync",
			lang:     "fr-FR",
			wantHTTP: http.StatusNotFound,
			wantCode: pkg.CodeNotFound,
			want:     "Order ord_missing was not found.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := newAPI(t, http.StatusCreated, `{"id":501}`, pkg.TenantBeta, time.Now().UTC())

			code, body := call(t, api, http.MethodPost, c.path, "", c.lang)

			if code != c.wantHTTP {
				t.Errorf("http status = %d, want %d", code, c.wantHTTP)
			}
			if body.Message != c.want {
				t.Errorf("message = %q, want %q", body.Message, c.want)
			}
			if strings.HasPrefix(body.Message, "sync.") {
				t.Errorf("raw message code leaked to the client: %q", body.Message)
			}

			detail, ok := body.Error.(map[string]any)
			if !ok {
				t.Fatalf("error detail = %T, want an object", body.Error)
			}
			if detail["code"] != c.wantCode {
				t.Errorf("error code = %v, want %s", detail["code"], c.wantCode)
			}
		})
	}
}

func TestBatchSyncValidation(t *testing.T) {
	manyIDs := `["` + strings.Repeat(`ord_1","`, 100) + `ord_1"]`

	cases := []struct {
		name        string
		body        string
		wantHTTP    int
		wantDetails []string
	}{
		{
			name:     "valid batch",
			body:     `{"order_ids":["ord_1"]}`,
			wantHTTP: http.StatusOK,
		},
		{
			name:        "empty list",
			body:        `{"order_ids":[]}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantDetails: []string{"order_ids is required"},
		},
		{
			name:        "missing field",
			body:        `{}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantDetails: []string{"order_ids is required"},
		},
		{
			name:        "blank entry",
			body:        `{"order_ids":["ord_1","   "]}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantDetails: []string{"order_ids[1] cannot be blank"},
		},
		{
			name:        "over the batch cap",
			body:        `{"order_ids":` + manyIDs + `}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantDetails: []string{"order_ids must contain at most 100 entries"},
		},
		{
			name:     "malformed json",
			body:     `{"order_ids":`,
			wantHTTP: http.StatusBadRequest,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := newAPI(t, http.StatusCreated, `{"id":501}`, pkg.TenantBeta, time.Now().UTC())

			code, body := call(t, api, http.MethodPost, "/api/v1/orders/batch-sync", c.body, "")

			if code != c.wantHTTP {
				t.Fatalf("http status = %d, want %d (body %+v)", code, c.wantHTTP, body)
			}
			if len(c.wantDetails) == 0 {
				return
			}

			detail, ok := body.Error.(map[string]any)
			if !ok {
				t.Fatalf("error detail = %T, want an object", body.Error)
			}

			details, _ := detail["details"].([]any)
			for _, want := range c.wantDetails {
				found := false
				for _, got := range details {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("details = %v, want to contain %q", details, want)
				}
			}
		})
	}
}

// A failure inside a batch is reported per order; the batch call itself still succeeds.
func TestBatchSyncReportsPerOrder(t *testing.T) {
	api := newAPI(t, http.StatusCreated, `{"id":501}`, pkg.TenantBeta, time.Now().UTC())

	code, body := call(t, api, http.MethodPost, "/api/v1/orders/batch-sync",
		`{"order_ids":["ord_1","ord_missing"]}`, "")

	if code != http.StatusOK {
		t.Fatalf("http status = %d, want 200", code)
	}

	results, ok := body.Data.([]any)
	if !ok {
		t.Fatalf("data = %T, want a list", body.Data)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	cases := []struct {
		name       string
		index      int
		wantStatus string
	}{
		{name: "existing order syncs", index: 0, wantStatus: pkg.StatusSynced},
		{name: "missing order fails in place", index: 1, wantStatus: pkg.StatusFailed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := results[c.index].(map[string]any)
			if result["status"] != c.wantStatus {
				t.Errorf("status = %v, want %s", result["status"], c.wantStatus)
			}
		})
	}
}

func TestGetSyncStatus(t *testing.T) {
	api := newAPI(t, http.StatusCreated, `{"id":501}`, pkg.TenantBeta, time.Now().UTC())

	if code, body := call(t, api, http.MethodPost, "/api/v1/orders/ord_1/sync", "", ""); code != http.StatusOK {
		t.Fatalf("seed sync: status = %d (%+v)", code, body)
	}

	cases := []struct {
		name     string
		path     string
		wantHTTP int
	}{
		{name: "synced order", path: "/api/v1/orders/ord_1/sync-status", wantHTTP: http.StatusOK},
		{name: "order never synced", path: "/api/v1/orders/ord_missing/sync-status", wantHTTP: http.StatusNotFound},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := call(t, api, http.MethodGet, c.path, "", "")

			if code != c.wantHTTP {
				t.Errorf("http status = %d, want %d", code, c.wantHTTP)
			}
			if body.Message == "" {
				t.Error("response carries no message")
			}
		})
	}
}
