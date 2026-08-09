package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"

	"zolo-test-integration/internal/pkg"
)

// Database tables.
type Table string

const (
	TableOrders          Table = "orders"
	TableOrderItems      Table = "order_items"
	TableSyncAttempts    Table = "sync_attempts"
	TableSyncLineResults Table = "sync_line_results"
)

type RepositoryOption struct {
	pkg.OptionsApplication
}

type Repository struct {
	Order IOrderRepository
	Sync  ISyncRepository
}

// TransactionWrapper runs fn inside a transaction, rolling back on error or
// panic. Every write path goes through it so a sync attempt and its line
// results are never half-written.
func TransactionWrapper(ctx context.Context, db *sqlx.DB, fn func(tx *sqlx.Tx) error) (err error) {
	if db == nil {
		return pkg.NewDatabaseError(fmt.Errorf("database connection not found"))
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return pkg.NewDatabaseError(err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			err = pkg.NewDatabaseError(fmt.Errorf("panic: %v", p))
		}
	}()

	if err = fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err = tx.Commit(); err != nil {
		return pkg.NewDatabaseError(err)
	}

	return nil
}
