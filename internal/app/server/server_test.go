package server_test

import (
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
	"zolo-test-integration/internal/mockerp"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/migrations"
	"zolo-test-integration/pkg/driver"
)

// newAPI mounts the full HTTP surface over the real stack and the real mock ERP, on the
// seeded database. Echo is an http.Handler, so nothing binds a port.
func newAPI(t *testing.T) (http.Handler, *mockerp.Server) {
	t.Helper()

	db, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{
		Path: filepath.Join(t.TempDir(), "zolo.db"),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.Run(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	logger := slog.New(slog.DiscardHandler)

	mock := mockerp.NewServer(logger)
	erpSrv := httptest.NewServer(mock.Handler())
	t.Cleanup(erpSrv.Close)

	erpCfg := config.ERP{
		AlphaBaseURL: erpSrv.URL,
		BetaBaseURL:  erpSrv.URL,
		Timeout:      5 * time.Second,
		MaxAttempts:  4,
		BackoffBase:  time.Millisecond,
		BackoffMax:   5 * time.Millisecond,
	}

	options := pkg.OptionsApplication{
		Config: &config.Config{
			Application: config.Application{Port: "0", DefaultLanguage: pkg.LangEN},
			ERP:         erpCfg,
		},
		DB:        db,
		Logger:    logger,
		Localizer: pkg.NewLocalizer(pkg.LangEN),
	}

	repoOpt := repository.RepositoryOption{OptionsApplication: options}
	repo := &repository.Repository{
		Order: repository.InitiateOrderRepository(repoOpt),
		Sync:  repository.InitiateSyncRepository(repoOpt),
	}

	svcOpt := service.ServiceOption{
		OptionsApplication: options,
		Repository:         repo,
		Registry:           tenant.DefaultRegistry(erpCfg),
		ERPClient:          erp.NewClient(erpCfg, logger),
	}

	svc := &service.Service{
		Sync:  service.InitiateSyncService(svcOpt),
		Order: service.InitiateOrderService(svcOpt),
	}

	return server.NewServer(options, svc).Handler(), mock
}

func request(t *testing.T, api http.Handler, method, path, body, lang string) (int, payload.BaseResponse) {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	var response payload.BaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response (%d): %v\n%s", rec.Code, err, rec.Body.String())
	}

	return rec.Code, response
}

func decodeResult(t *testing.T, data any) payload.SyncResult {
	t.Helper()

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var result payload.SyncResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	return result
}

// The seeded orders drive every branch against the real mock, so chunking and the 429
// backoff are exercised rather than stubbed.
func TestSyncEndpoint(t *testing.T) {
	cases := []struct {
		name         string
		orderID      string
		wantHTTP     int
		wantStatus   string
		wantInMsg    string
		wantERPCalls int
	}{
		{
			// Three lines exceed ERP A's two-per-request limit, so this only passes if
			// the mapper chunked: an unchunked request comes back 429.
			name:         "alpha order chunks and syncs",
			orderID:      "ord_998123",
			wantHTTP:     http.StatusOK,
			wantStatus:   pkg.StatusSynced,
			wantInMsg:    "ord_998123",
			wantERPCalls: 2,
		},
		{
			name:         "beta order partly accepted is 207",
			orderID:      "ord_998200",
			wantHTTP:     http.StatusMultiStatus,
			wantStatus:   pkg.StatusPartialSuccess,
			wantInMsg:    "SKU-OUTOFSTOCK",
			wantERPCalls: 1,
		},
		{
			name:      "expired order is rejected before dispatch",
			orderID:   "ord_expired",
			wantHTTP:  http.StatusUnprocessableEntity,
			wantInMsg: "24 hours",
		},
		{
			name:      "unmappable customer reference",
			orderID:   "ord_998201",
			wantHTTP:  http.StatusUnprocessableEntity,
			wantInMsg: "WALKIN",
		},
		{
			name:      "unknown order",
			orderID:   "ord_nope",
			wantHTTP:  http.StatusNotFound,
			wantInMsg: "ord_nope",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, mock := newAPI(t)

			code, body := request(t, api, http.MethodPost, "/api/v1/orders/"+c.orderID+"/sync", "", "")

			if code != c.wantHTTP {
				t.Fatalf("status = %d, want %d (%+v)", code, c.wantHTTP, body)
			}
			if !strings.Contains(body.Message, c.wantInMsg) {
				t.Errorf("message = %q, want it to mention %q", body.Message, c.wantInMsg)
			}
			if c.wantStatus != "" {
				if got := decodeResult(t, body.Data).Status; got != c.wantStatus {
					t.Errorf("result status = %s, want %s", got, c.wantStatus)
				}
			}

			erpCalls := mock.Calls("/api/v1/sap-adapter/orders") + mock.Calls("/api/v2/odoo-adapter/sales-order")
			if erpCalls != c.wantERPCalls {
				t.Errorf("erp calls = %d, want %d", erpCalls, c.wantERPCalls)
			}
		})
	}
}

// A repeat click must not book the order in the ERP twice.
func TestSyncEndpointIsIdempotent(t *testing.T) {
	api, mock := newAPI(t)

	cases := []struct {
		name         string
		wantReplayed bool
	}{
		{name: "first click dispatches"},
		{name: "second click replays", wantReplayed: true},
		{name: "third click replays", wantReplayed: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := request(t, api, http.MethodPost, "/api/v1/orders/ord_998123/sync", "", "")

			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%+v)", code, body)
			}
			if got := decodeResult(t, body.Data).Replayed; got != c.wantReplayed {
				t.Errorf("replayed = %v, want %v", got, c.wantReplayed)
			}
		})
	}

	if got := mock.Calls("/api/v1/sap-adapter/orders"); got != 2 {
		t.Errorf("erp calls = %d, want 2 (the two chunks of a single dispatch)", got)
	}
}

func TestBatchSyncEndpoint(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantHTTP    int
		wantStatus  []string
		wantInError string
	}{
		{
			name:       "mixed batch reports each order",
			body:       `{"order_ids":["ord_998123","ord_expired"]}`,
			wantHTTP:   http.StatusOK,
			wantStatus: []string{pkg.StatusSynced, pkg.StatusFailed},
		},
		{
			name:        "empty list is rejected",
			body:        `{"order_ids":[]}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantInError: "order_ids",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, _ := newAPI(t)

			code, body := request(t, api, http.MethodPost, "/api/v1/orders/batch-sync", c.body, "")

			if code != c.wantHTTP {
				t.Fatalf("status = %d, want %d (%+v)", code, c.wantHTTP, body)
			}

			if c.wantInError != "" {
				detail, _ := json.Marshal(body.Error)
				if !strings.Contains(string(detail), c.wantInError) {
					t.Errorf("error detail = %s, want it to name %q", detail, c.wantInError)
				}
				return
			}

			raw, _ := json.Marshal(body.Data)
			var results []payload.SyncResult
			if err := json.Unmarshal(raw, &results); err != nil {
				t.Fatalf("decode results: %v", err)
			}
			if len(results) != len(c.wantStatus) {
				t.Fatalf("results = %d, want %d", len(results), len(c.wantStatus))
			}
			for i, want := range c.wantStatus {
				if results[i].Status != want {
					t.Errorf("results[%d].status = %s, want %s", i, results[i].Status, want)
				}
			}
		})
	}
}

func TestSyncStatusEndpoint(t *testing.T) {
	api, _ := newAPI(t)

	if code, body := request(t, api, http.MethodPost, "/api/v1/orders/ord_998200/sync", "", ""); code != http.StatusMultiStatus {
		t.Fatalf("setup sync returned %d (%+v)", code, body)
	}

	cases := []struct {
		name       string
		orderID    string
		wantHTTP   int
		wantStatus string
		wantLines  int
	}{
		{
			name:       "returns the stored audit row",
			orderID:    "ord_998200",
			wantHTTP:   http.StatusOK,
			wantStatus: pkg.StatusPartialSuccess,
			wantLines:  2,
		},
		{
			name:     "order that was never synced",
			orderID:  "ord_998124",
			wantHTTP: http.StatusNotFound,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := request(t, api, http.MethodGet, "/api/v1/orders/"+c.orderID+"/sync-status", "", "")

			if code != c.wantHTTP {
				t.Fatalf("status = %d, want %d (%+v)", code, c.wantHTTP, body)
			}
			if c.wantStatus == "" {
				return
			}

			result := decodeResult(t, body.Data)
			if result.Status != c.wantStatus {
				t.Errorf("status = %s, want %s", result.Status, c.wantStatus)
			}
			if len(result.Lines) != c.wantLines {
				t.Errorf("lines = %d, want %d", len(result.Lines), c.wantLines)
			}
		})
	}
}

// Keeping message codes out of the service layer is what lets the edge answer in the
// caller's language.
func TestResponsesAreLocalized(t *testing.T) {
	cases := []struct {
		name      string
		lang      string
		wantInMsg string
	}{
		{name: "default is english", lang: "", wantInMsg: "24 hours"},
		{name: "explicit english", lang: "en", wantInMsg: "24 hours"},
		{name: "malay with q-weights", lang: "ms-MY,ms;q=0.9,en;q=0.8", wantInMsg: "Pesanan"},
		{name: "unsupported falls back", lang: "fr-FR", wantInMsg: "24 hours"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, _ := newAPI(t)

			_, body := request(t, api, http.MethodPost, "/api/v1/orders/ord_expired/sync", "", c.lang)

			if !strings.Contains(body.Message, c.wantInMsg) {
				t.Errorf("message = %q, want it to contain %q", body.Message, c.wantInMsg)
			}

			detail, _ := json.Marshal(body.Error)
			if !strings.Contains(string(detail), pkg.CodeOrderExpired) {
				t.Errorf("error payload lost the machine-readable code: %s", detail)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	api, _ := newAPI(t)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
