package repository_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"zolo-test-integration/internal/app/repository"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/migrations"
	"zolo-test-integration/pkg/driver"
)

const testOrderID = "ord_1"

// newSyncRepo gives each test its own migrated database file. In-memory SQLite would
// hand every connection in the pool a separate empty database.
func newSyncRepo(t *testing.T) repository.ISyncRepository {
	t.Helper()

	db, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.Run(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO orders (order_id, tenant_id, status, confirmed_at, confirmed_by_user_id, currency)
		VALUES (?, ?, 'CONFIRMED', CURRENT_TIMESTAMP, 'user_1', 'MYR')`,
		testOrderID, pkg.TenantAlpha)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}

	return repository.InitiateSyncRepository(repository.RepositoryOption{
		OptionsApplication: pkg.OptionsApplication{DB: db, Logger: slog.New(slog.DiscardHandler)},
	})
}

func key(t *testing.T) string {
	t.Helper()
	return model.IdempotencyKey(testOrderID, pkg.TenantAlpha)
}

// complete drives an attempt to a terminal state the way the service does.
func complete(t *testing.T, repo repository.ISyncRepository, attempt model.SyncAttempt, status string, lines ...model.SyncLineResult) {
	t.Helper()

	attempt.Status = status
	attempt.Lines = lines
	if err := repo.CompleteAttempt(context.Background(), attempt); err != nil {
		t.Fatalf("CompleteAttempt: %v", err)
	}
}

func TestClaimAttemptFirstClaim(t *testing.T) {
	repo := newSyncRepo(t)

	attempt, claimed, err := repo.ClaimAttempt(context.Background(), testOrderID, pkg.TenantAlpha, key(t))
	if err != nil {
		t.Fatalf("ClaimAttempt: %v", err)
	}

	if !claimed {
		t.Error("first claim did not win the row")
	}
	if attempt.Status != pkg.StatusPending {
		t.Errorf("status = %s, want %s", attempt.Status, pkg.StatusPending)
	}
	if attempt.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", attempt.AttemptCount)
	}
	if attempt.ID == 0 {
		t.Error("claimed attempt has no id")
	}
}

// The second claim is what a double-click looks like. Whether it may dispatch depends
// entirely on the state the first one left behind.
func TestClaimAttemptReclaim(t *testing.T) {
	cases := []struct {
		name        string
		firstStatus string
		wantClaimed bool
		wantCount   int
	}{
		{name: "pending claim is still owned", firstStatus: pkg.StatusPending, wantCount: 1},
		{name: "synced order never re-dispatches", firstStatus: pkg.StatusSynced, wantCount: 1},
		{name: "failed attempt is retryable", firstStatus: pkg.StatusFailed, wantClaimed: true, wantCount: 2},
		{name: "partial success is retryable", firstStatus: pkg.StatusPartialSuccess, wantClaimed: true, wantCount: 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newSyncRepo(t)
			ctx := context.Background()

			first, claimed, err := repo.ClaimAttempt(ctx, testOrderID, pkg.TenantAlpha, key(t))
			if err != nil || !claimed {
				t.Fatalf("first ClaimAttempt: claimed=%v err=%v", claimed, err)
			}

			if c.firstStatus != pkg.StatusPending {
				complete(t, repo, first, c.firstStatus)
			}

			attempt, claimed, err := repo.ClaimAttempt(ctx, testOrderID, pkg.TenantAlpha, key(t))
			if err != nil {
				t.Fatalf("second ClaimAttempt: %v", err)
			}

			if claimed != c.wantClaimed {
				t.Errorf("claimed = %v, want %v", claimed, c.wantClaimed)
			}
			if attempt.AttemptCount != c.wantCount {
				t.Errorf("attempt_count = %d, want %d", attempt.AttemptCount, c.wantCount)
			}
			if attempt.ID != first.ID {
				t.Errorf("id = %d, want the original row %d", attempt.ID, first.ID)
			}
			if !c.wantClaimed && attempt.Status != c.firstStatus {
				t.Errorf("status = %s, want the untouched %s", attempt.Status, c.firstStatus)
			}
			if c.wantClaimed && attempt.Status != pkg.StatusPending {
				t.Errorf("status = %s, want %s after a reclaim", attempt.Status, pkg.StatusPending)
			}
		})
	}
}

// Concurrent double-clicks: exactly one may dispatch, or the order reaches the ERP twice.
func TestClaimAttemptIsExclusiveUnderConcurrency(t *testing.T) {
	cases := []struct {
		name    string
		callers int
	}{
		{name: "double click", callers: 2},
		{name: "impatient user", callers: 8},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newSyncRepo(t)

			var (
				wg      sync.WaitGroup
				mu      sync.Mutex
				winners int
			)

			wg.Add(c.callers)
			for range c.callers {
				go func() {
					defer wg.Done()

					_, claimed, err := repo.ClaimAttempt(context.Background(), testOrderID, pkg.TenantAlpha, key(t))
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						t.Errorf("ClaimAttempt: %v", err)
						return
					}
					if claimed {
						winners++
					}
				}()
			}
			wg.Wait()

			if winners != 1 {
				t.Errorf("claims won = %d, want exactly 1", winners)
			}
		})
	}
}

func TestCompleteAttempt(t *testing.T) {
	reason := "OUT_OF_STOCK"

	cases := []struct {
		name         string
		status       string
		lines        []model.SyncLineResult
		wantRejected []string
	}{
		{
			name:   "synced with all lines accepted",
			status: pkg.StatusSynced,
			lines: []model.SyncLineResult{
				{SKU: "SKU-1", Accepted: true},
				{SKU: "SKU-2", Accepted: true},
			},
		},
		{
			name:   "partial success records the rejected sku",
			status: pkg.StatusPartialSuccess,
			lines: []model.SyncLineResult{
				{SKU: "SKU-1", Accepted: true},
				{SKU: "SKU-2", Reason: &reason},
			},
			wantRejected: []string{"SKU-2"},
		},
		{
			name:   "failed with no line detail",
			status: pkg.StatusFailed,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newSyncRepo(t)
			ctx := context.Background()

			attempt, _, err := repo.ClaimAttempt(ctx, testOrderID, pkg.TenantAlpha, key(t))
			if err != nil {
				t.Fatalf("ClaimAttempt: %v", err)
			}

			complete(t, repo, attempt, c.status, c.lines...)

			stored, err := repo.GetLatestAttemptByOrderID(ctx, testOrderID)
			if err != nil {
				t.Fatalf("GetLatestAttemptByOrderID: %v", err)
			}

			if stored.Status != c.status {
				t.Errorf("status = %s, want %s", stored.Status, c.status)
			}
			if len(stored.Lines) != len(c.lines) {
				t.Fatalf("lines = %d, want %d", len(stored.Lines), len(c.lines))
			}
			if got := stored.RejectedSKUs(); len(got) != len(c.wantRejected) {
				t.Errorf("rejected SKUs = %v, want %v", got, c.wantRejected)
			}
			for _, line := range stored.Lines {
				if !line.Accepted && (line.Reason == nil || *line.Reason == "") {
					t.Errorf("rejected SKU %s lost its reason in storage", line.SKU)
				}
			}
		})
	}
}

// A retry must not leave the previous run's line results behind, or the audit trail
// disagrees with the response snapshot stored beside it.
func TestCompleteAttemptReplacesLineResults(t *testing.T) {
	repo := newSyncRepo(t)
	ctx := context.Background()

	reason := "OUT_OF_STOCK"

	attempt, _, err := repo.ClaimAttempt(ctx, testOrderID, pkg.TenantAlpha, key(t))
	if err != nil {
		t.Fatalf("ClaimAttempt: %v", err)
	}
	complete(t, repo, attempt, pkg.StatusPartialSuccess,
		model.SyncLineResult{SKU: "SKU-1", Accepted: true},
		model.SyncLineResult{SKU: "SKU-2", Reason: &reason},
	)

	retried, claimed, err := repo.ClaimAttempt(ctx, testOrderID, pkg.TenantAlpha, key(t))
	if err != nil || !claimed {
		t.Fatalf("reclaim: claimed=%v err=%v", claimed, err)
	}
	complete(t, repo, retried, pkg.StatusSynced,
		model.SyncLineResult{SKU: "SKU-1", Accepted: true},
		model.SyncLineResult{SKU: "SKU-2", Accepted: true},
	)

	stored, err := repo.GetAttemptByKey(ctx, key(t))
	if err != nil {
		t.Fatalf("GetAttemptByKey: %v", err)
	}

	if len(stored.Lines) != 2 {
		t.Fatalf("lines = %d, want 2 (stale rows were not replaced)", len(stored.Lines))
	}
	if got := stored.RejectedSKUs(); len(got) != 0 {
		t.Errorf("rejected SKUs = %v, want none after a successful retry", got)
	}
	if stored.AttemptCount != 2 {
		t.Errorf("attempt_count = %d, want 2", stored.AttemptCount)
	}
}

func TestGetAttemptNotFound(t *testing.T) {
	repo := newSyncRepo(t)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "by idempotency key",
			call: func() error {
				_, err := repo.GetAttemptByKey(ctx, "no-such-key")
				return err
			},
		},
		{
			name: "by order id",
			call: func() error {
				_, err := repo.GetLatestAttemptByOrderID(ctx, "ord_missing")
				return err
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatal("want not-found error, got nil")
			}

			appErr, ok := err.(*pkg.AppError)
			if !ok {
				t.Fatalf("error type = %T, want *pkg.AppError", err)
			}
			if appErr.Code != pkg.CodeNotFound {
				t.Errorf("code = %s, want %s", appErr.Code, pkg.CodeNotFound)
			}
		})
	}
}
