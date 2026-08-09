package migrations_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/migrations"
	"zolo-test-integration/pkg/driver"
)

func migrated(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.Run(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

// Run is called on every boot, so it has to be safe to repeat.
func TestRunIsIdempotent(t *testing.T) {
	db := migrated(t)

	if err := migrations.Run(db); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var orders int
	if err := db.Get(&orders, `SELECT COUNT(*) FROM orders`); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if orders != 5 {
		t.Errorf("orders = %d, want 5 (seed applied twice?)", orders)
	}
}

// Each seeded order exists to exercise one branch of the sync flow. If a row drifts, the
// local demo silently stops covering that case.
func TestSeedCoversTheInterestingCases(t *testing.T) {
	db := migrated(t)

	cases := []struct {
		name        string
		orderID     string
		wantTenant  string
		wantLines   int
		wantExpired bool
		why         string
	}{
		{
			name:       "alpha order that must chunk",
			orderID:    "ord_998123",
			wantTenant: pkg.TenantAlpha,
			wantLines:  3,
			why:        "three lines cross ERP A's two-per-request limit",
		},
		{
			name:       "alpha order identified by phone",
			orderID:    "ord_998124",
			wantTenant: pkg.TenantAlpha,
			wantLines:  1,
			why:        "no external_ref, so Customer_ID falls back to the phone",
		},
		{
			name:       "beta order with a rejected line",
			orderID:    "ord_998200",
			wantTenant: pkg.TenantBeta,
			wantLines:  2,
			why:        "SKU-OUTOFSTOCK makes ERP B answer 207",
		},
		{
			name:       "beta order with an unmappable ref",
			orderID:    "ord_998201",
			wantTenant: pkg.TenantBeta,
			wantLines:  1,
			why:        "WALKIN has no PREFIX-NUMBER form, so pre-validation rejects it",
		},
		{
			name:        "beta order past the expiry window",
			orderID:     "ord_expired",
			wantTenant:  pkg.TenantBeta,
			wantLines:   1,
			wantExpired: true,
			why:         "confirmed 30 hours ago, past ERP B's 24-hour limit",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var row struct {
				TenantID    string    `db:"tenant_id"`
				Status      string    `db:"status"`
				ConfirmedAt time.Time `db:"confirmed_at"`
			}
			err := db.Get(&row, `SELECT tenant_id, status, confirmed_at FROM orders WHERE order_id = ?`, c.orderID)
			if err != nil {
				t.Fatalf("%s missing from the seed (%s): %v", c.orderID, c.why, err)
			}

			if row.TenantID != c.wantTenant {
				t.Errorf("tenant = %s, want %s", row.TenantID, c.wantTenant)
			}
			if row.Status != pkg.OrderStatusConfirmed {
				t.Errorf("status = %s, want %s", row.Status, pkg.OrderStatusConfirmed)
			}

			expired := time.Since(row.ConfirmedAt) > tenant.MaxOrderAge
			if expired != c.wantExpired {
				t.Errorf("expired = %v, want %v (confirmed_at %s)", expired, c.wantExpired, row.ConfirmedAt)
			}

			var lines int
			if err := db.Get(&lines, `SELECT COUNT(*) FROM order_items WHERE order_id = ?`, c.orderID); err != nil {
				t.Fatalf("count items: %v", err)
			}
			if lines != c.wantLines {
				t.Errorf("line items = %d, want %d (%s)", lines, c.wantLines, c.why)
			}
		})
	}
}

// The schema constraints are the last line of defence for money and status values.
func TestSchemaRejectsBadRows(t *testing.T) {
	cases := []struct {
		name string
		stmt string
		args []any
	}{
		{
			name: "unknown sync status",
			stmt: `INSERT INTO sync_attempts (order_id, tenant_id, idempotency_key, status) VALUES ('ord_998123', 'tenant_alpha', 'k1', 'WHATEVER')`,
		},
		{
			name: "duplicate idempotency key",
			stmt: `INSERT INTO sync_attempts (order_id, tenant_id, idempotency_key, status)
			       VALUES ('ord_998123', 'tenant_alpha', 'dup', 'PENDING'), ('ord_998124', 'tenant_alpha', 'dup', 'PENDING')`,
		},
		{
			name: "negative quantity",
			stmt: `INSERT INTO order_items (order_id, line_no, sku, qty, unit_price_cents) VALUES ('ord_998123', 9, 'SKU-X', -1, 100)`,
		},
		{
			name: "negative price",
			stmt: `INSERT INTO order_items (order_id, line_no, sku, qty, unit_price_cents) VALUES ('ord_998123', 9, 'SKU-X', 1, -100)`,
		},
		{
			name: "discount over 100 percent",
			stmt: `INSERT INTO order_items (order_id, line_no, sku, qty, unit_price_cents, discount_bps) VALUES ('ord_998123', 9, 'SKU-X', 1, 100, 10001)`,
		},
		{
			name: "line item without an order",
			stmt: `INSERT INTO order_items (order_id, line_no, sku, qty, unit_price_cents) VALUES ('ord_nope', 1, 'SKU-X', 1, 100)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := migrated(t)

			if _, err := db.Exec(c.stmt, c.args...); err == nil {
				t.Error("insert succeeded, want a constraint violation")
			}
		})
	}
}
