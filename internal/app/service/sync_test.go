package service_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository"
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/migrations"
	"zolo-test-integration/pkg/driver"
)

type seedOrder struct {
	orderID     string
	tenantID    string
	status      string
	externalRef string
	confirmedAt time.Time
	items       []seedItem
}

type seedItem struct {
	sku   string
	qty   int64
	price int64
	bps   int64
}

func defaultOrder(tenantID string) seedOrder {
	return seedOrder{
		orderID:     "ord_1",
		tenantID:    tenantID,
		status:      pkg.OrderStatusConfirmed,
		externalRef: "CUST-882",
		confirmedAt: time.Now().UTC(),
		items: []seedItem{
			{sku: "SKU-1", qty: 10, price: 1850, bps: 1000},
			{sku: "SKU-2", qty: 6, price: 1000},
		},
	}
}

// newService builds the real stack against a temp database and a stub ERP, so the
// test exercises the actual mappers, dispatcher and persistence.
func newService(t *testing.T, erpURL string, orders ...seedOrder) service.ISyncService {
	t.Helper()

	db, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.Run(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, o := range orders {
		_, err := db.Exec(`
			INSERT INTO orders (order_id, tenant_id, status, confirmed_at, confirmed_by_user_id, currency, customer_external_ref)
			VALUES (?, ?, ?, ?, 'user_1', 'MYR', ?)`,
			o.orderID, o.tenantID, o.status, o.confirmedAt, o.externalRef)
		if err != nil {
			t.Fatalf("seed order: %v", err)
		}

		for i, item := range o.items {
			_, err := db.Exec(`
				INSERT INTO order_items (order_id, line_no, sku, qty, unit_price_cents, discount_bps)
				VALUES (?, ?, ?, ?, ?, ?)`,
				o.orderID, i+1, item.sku, item.qty, item.price, item.bps)
			if err != nil {
				t.Fatalf("seed item: %v", err)
			}
		}
	}

	options := pkg.OptionsApplication{DB: db, Logger: slog.New(slog.DiscardHandler)}
	cfg := config.ERP{
		AlphaBaseURL: erpURL,
		BetaBaseURL:  erpURL,
		Timeout:      2 * time.Second,
		MaxAttempts:  2,
		BackoffBase:  time.Millisecond,
		BackoffMax:   5 * time.Millisecond,
	}

	return service.InitiateSyncService(service.ServiceOption{
		OptionsApplication: options,
		Repository: &repository.Repository{
			Order: repository.InitiateOrderRepository(repository.RepositoryOption{OptionsApplication: options}),
			Sync:  repository.InitiateSyncRepository(repository.RepositoryOption{OptionsApplication: options}),
		},
		Registry:  tenant.DefaultRegistry(cfg),
		ERPClient: erp.NewClient(cfg, options.Logger),
	})
}

func stubERP(t *testing.T, status int, body string, calls *atomic.Int32) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

func TestSyncOrder(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		erpStatus  int
		erpBody    string
		wantStatus string
		wantCalls  int32 // ERP A chunks two lines into one request, ERP B sends one
		wantLines  int
	}{
		{
			name:       "alpha order accepted",
			tenantID:   pkg.TenantAlpha,
			erpStatus:  http.StatusCreated,
			erpBody:    `{"erp_order_id":"SAP-1"}`,
			wantStatus: pkg.StatusSynced,
			wantCalls:  1,
			wantLines:  2,
		},
		{
			name:       "beta order accepted",
			tenantID:   pkg.TenantBeta,
			erpStatus:  http.StatusCreated,
			erpBody:    `{"id":501}`,
			wantStatus: pkg.StatusSynced,
			wantCalls:  1,
			wantLines:  2,
		},
		{
			name:       "beta partial success",
			tenantID:   pkg.TenantBeta,
			erpStatus:  http.StatusMultiStatus,
			erpBody:    `{"line_results":[{"product_code":"SKU-1","status":"success"},{"product_code":"SKU-2","status":"rejected","reason":"OUT_OF_STOCK"}]}`,
			wantStatus: pkg.StatusPartialSuccess,
			wantCalls:  1,
			wantLines:  2,
		},
		{
			name:       "erp rejects the order",
			tenantID:   pkg.TenantAlpha,
			erpStatus:  http.StatusBadRequest,
			erpBody:    `{"error":"SKU_NOT_FOUND"}`,
			wantStatus: pkg.StatusFailed,
			wantCalls:  1,
			wantLines:  2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			ctx := context.Background()

			svc := newService(t, stubERP(t, c.erpStatus, c.erpBody, &calls), defaultOrder(c.tenantID))

			result, err := svc.SyncOrder(ctx, "ord_1")
			if err != nil {
				t.Fatalf("SyncOrder: %v", err)
			}

			if result.Status != c.wantStatus {
				t.Errorf("status = %s, want %s", result.Status, c.wantStatus)
			}
			if got := calls.Load(); got != c.wantCalls {
				t.Errorf("erp calls = %d, want %d", got, c.wantCalls)
			}
			if len(result.Lines) != c.wantLines {
				t.Errorf("lines = %d, want %d", len(result.Lines), c.wantLines)
			}
			if result.Replayed {
				t.Error("a fresh sync must not be marked replayed")
			}

			// The audit row is what the dashboard reads afterwards.
			stored, err := svc.GetSyncStatus(ctx, "ord_1")
			if err != nil {
				t.Fatalf("GetSyncStatus: %v", err)
			}
			if stored.Status != c.wantStatus {
				t.Errorf("persisted status = %s, want %s", stored.Status, c.wantStatus)
			}
			if stored.AttemptCount != 1 {
				t.Errorf("persisted attempt_count = %d, want 1", stored.AttemptCount)
			}
		})
	}
}

// Orders that can never succeed are rejected before the claim, so they neither burn an
// idempotency key nor reach the network.
func TestSyncOrderRejectedBeforeDispatch(t *testing.T) {
	stale := time.Now().UTC().Add(-48 * time.Hour)

	cases := []struct {
		name     string
		order    seedOrder
		wantCode string
	}{
		{
			name: "order not confirmed",
			order: func() seedOrder {
				o := defaultOrder(pkg.TenantBeta)
				o.status = "DRAFT"
				return o
			}(),
			wantCode: pkg.CodeValidationError,
		},
		{
			name: "unknown tenant",
			order: func() seedOrder {
				o := defaultOrder("tenant_gamma")
				return o
			}(),
			wantCode: pkg.CodeUnknownTenant,
		},
		{
			name: "expired beta order",
			order: func() seedOrder {
				o := defaultOrder(pkg.TenantBeta)
				o.confirmedAt = stale
				return o
			}(),
			wantCode: pkg.CodeOrderExpired,
		},
		{
			name: "unmappable partner ref",
			order: func() seedOrder {
				o := defaultOrder(pkg.TenantBeta)
				o.externalRef = "WALK-IN"
				return o
			}(),
			wantCode: pkg.CodeValidationError,
		},
		{
			name: "order with no line items",
			order: func() seedOrder {
				o := defaultOrder(pkg.TenantAlpha)
				o.items = nil
				return o
			}(),
			wantCode: pkg.CodeValidationError,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			svc := newService(t, stubERP(t, http.StatusCreated, `{}`, &calls), c.order)

			_, err := svc.SyncOrder(context.Background(), "ord_1")
			if err == nil {
				t.Fatal("SyncOrder: want error, got nil")
			}

			appErr, ok := err.(*pkg.AppError)
			if !ok {
				t.Fatalf("error type = %T, want *pkg.AppError", err)
			}
			if appErr.Code != c.wantCode {
				t.Errorf("code = %s, want %s", appErr.Code, c.wantCode)
			}
			if got := calls.Load(); got != 0 {
				t.Errorf("erp calls = %d, want 0 (rejected before dispatch)", got)
			}
		})
	}
}

// A synced order is never pushed twice, however many times the button is clicked.
func TestSyncOrderIsIdempotent(t *testing.T) {
	cases := []struct {
		name       string
		erpStatus  int
		erpBody    string
		wantStatus string
		wantCalls  int32
	}{
		{
			name:       "synced order replays",
			erpStatus:  http.StatusCreated,
			erpBody:    `{"id":501}`,
			wantStatus: pkg.StatusSynced,
			wantCalls:  1,
		},
		{
			// A failed attempt is retryable, so the second click does reach the ERP.
			name:       "failed order retries",
			erpStatus:  http.StatusBadRequest,
			erpBody:    `{"error":"SKU_NOT_FOUND"}`,
			wantStatus: pkg.StatusFailed,
			wantCalls:  2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			ctx := context.Background()

			svc := newService(t, stubERP(t, c.erpStatus, c.erpBody, &calls), defaultOrder(pkg.TenantBeta))

			first, err := svc.SyncOrder(ctx, "ord_1")
			if err != nil {
				t.Fatalf("first SyncOrder: %v", err)
			}

			second, err := svc.SyncOrder(ctx, "ord_1")
			if err != nil {
				t.Fatalf("second SyncOrder: %v", err)
			}

			if first.Status != c.wantStatus || second.Status != c.wantStatus {
				t.Errorf("statuses = %s, %s, want %s", first.Status, second.Status, c.wantStatus)
			}
			if got := calls.Load(); got != c.wantCalls {
				t.Errorf("erp calls = %d, want %d", got, c.wantCalls)
			}
			if wantReplay := c.wantCalls == 1; second.Replayed != wantReplay {
				t.Errorf("second call replayed = %v, want %v", second.Replayed, wantReplay)
			}
		})
	}
}

// Concurrent clicks: one dispatches, the rest either replay or get a conflict, but the
// order must never reach the ERP twice.
func TestSyncOrderConcurrentClicks(t *testing.T) {
	var calls atomic.Int32

	// A slow ERP widens the window where a second request sees a PENDING claim.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":501}`))
	}))
	defer srv.Close()

	svc := newService(t, srv.URL, defaultOrder(pkg.TenantBeta))

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		conflicts int
		succeeded int
	)

	wg.Add(4)
	for range 4 {
		go func() {
			defer wg.Done()

			_, err := svc.SyncOrder(context.Background(), "ord_1")

			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				succeeded++
				return
			}
			if appErr, ok := err.(*pkg.AppError); ok && appErr.Code == pkg.CodeSyncInProgress {
				conflicts++
				return
			}
			t.Errorf("unexpected error: %v", err)
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("erp calls = %d, want exactly 1", got)
	}
	if succeeded+conflicts != 4 {
		t.Errorf("accounted results = %d, want 4", succeeded+conflicts)
	}
	if succeeded < 1 {
		t.Error("no request completed the sync")
	}
}

func TestBatchSyncOrders(t *testing.T) {
	confirmed := defaultOrder(pkg.TenantBeta)

	expired := defaultOrder(pkg.TenantBeta)
	expired.orderID = "ord_2"
	expired.confirmedAt = time.Now().UTC().Add(-48 * time.Hour)

	second := defaultOrder(pkg.TenantAlpha)
	second.orderID = "ord_3"

	cases := []struct {
		name        string
		orderIDs    []string
		wantStatus  []string
		wantResults int
	}{
		{
			name:        "all orders sync",
			orderIDs:    []string{"ord_1", "ord_3"},
			wantStatus:  []string{pkg.StatusSynced, pkg.StatusSynced},
			wantResults: 2,
		},
		{
			// One bad order must not hide the outcome of the others.
			name:        "one expired order does not abort the batch",
			orderIDs:    []string{"ord_1", "ord_2", "ord_3"},
			wantStatus:  []string{pkg.StatusSynced, pkg.StatusFailed, pkg.StatusSynced},
			wantResults: 3,
		},
		{
			name:        "unknown order still returns a row",
			orderIDs:    []string{"ord_1", "ord_missing"},
			wantStatus:  []string{pkg.StatusSynced, pkg.StatusFailed},
			wantResults: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			svc := newService(t, stubERP(t, http.StatusCreated, `{"id":501}`, &calls), confirmed, expired, second)

			results, err := svc.BatchSyncOrders(context.Background(), c.orderIDs)
			if err != nil {
				t.Fatalf("BatchSyncOrders: %v", err)
			}

			if len(results) != c.wantResults {
				t.Fatalf("results = %d, want %d", len(results), c.wantResults)
			}
			for i, result := range results {
				if result.Status != c.wantStatus[i] {
					t.Errorf("results[%d].status = %s, want %s", i, result.Status, c.wantStatus[i])
				}
				if result.OrderID != c.orderIDs[i] {
					t.Errorf("results[%d].order_id = %s, want %s", i, result.OrderID, c.orderIDs[i])
				}
			}
		})
	}
}

// The snapshots are the audit trail an incident is reconstructed from.
func TestSyncOrderRecordsSnapshots(t *testing.T) {
	var calls atomic.Int32
	ctx := context.Background()

	svc := newService(t, stubERP(t, http.StatusCreated, `{"id":501}`, &calls), defaultOrder(pkg.TenantBeta))

	if _, err := svc.SyncOrder(ctx, "ord_1"); err != nil {
		t.Fatalf("SyncOrder: %v", err)
	}

	stored, err := svc.GetSyncStatus(ctx, "ord_1")
	if err != nil {
		t.Fatalf("GetSyncStatus: %v", err)
	}

	cases := []struct {
		name string
		got  string
	}{
		{name: "status", got: stored.Status},
		{name: "tenant id", got: stored.TenantID},
		{name: "message code", got: stored.MessageCode},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got == "" {
				t.Errorf("%s was not persisted", c.name)
			}
		})
	}

	if len(stored.Lines) != 2 {
		t.Errorf("stored lines = %d, want 2", len(stored.Lines))
	}
}

func TestHTTPStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   int
	}{
		{name: "synced", status: pkg.StatusSynced, want: http.StatusOK},
		{name: "partial success", status: pkg.StatusPartialSuccess, want: http.StatusMultiStatus},
		{name: "failed", status: pkg.StatusFailed, want: http.StatusBadGateway},
		{name: "pending", status: pkg.StatusPending, want: http.StatusBadGateway},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := service.HTTPStatus(c.status); got != c.want {
				t.Errorf("HTTPStatus(%s) = %d, want %d", c.status, got, c.want)
			}
		})
	}
}
