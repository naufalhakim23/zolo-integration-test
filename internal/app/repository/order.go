package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
)

type (
	IOrderRepository interface {
		CreateOrder(ctx context.Context, order model.Order) error
		GetOrderByID(ctx context.Context, orderID string) (model.Order, error)
		GetItemsByOrderID(ctx context.Context, orderID string) ([]model.OrderItem, error)
	}

	OrderRepository struct {
		RepositoryOption
	}
)

func InitiateOrderRepository(opt RepositoryOption) IOrderRepository {
	return &OrderRepository{RepositoryOption: opt}
}

// CreateOrder stores a confirmed order and its lines in one transaction, so a sync can
// never read an order whose items are still being written.
func (r *OrderRepository) CreateOrder(ctx context.Context, order model.Order) error {
	insertOrder := fmt.Sprintf(`
	INSERT INTO %s (order_id, tenant_id, status, confirmed_at, confirmed_by_user_id, currency, customer_phone, customer_external_ref)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (order_id) DO NOTHING`, TableOrders)

	insertItem := fmt.Sprintf(`
	INSERT INTO %s (order_id, line_no, sku, qty, unit_price_cents, discount_bps)
	VALUES (?, ?, ?, ?, ?, ?)`, TableOrderItems)

	return TransactionWrapper(ctx, r.DB, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx, tx.Rebind(insertOrder),
			order.OrderID,
			order.TenantID,
			order.Status,
			order.ConfirmedAt,
			order.ConfirmedByUserID,
			order.Currency,
			order.CustomerPhone,
			order.CustomerExternalRef,
		)
		if err != nil {
			return pkg.NewDatabaseError(err)
		}

		// A re-post of the same order_id is a duplicate confirmation, not an update.
		// Overwriting would silently change what a completed sync attempt was built from.
		if rows, _ := res.RowsAffected(); rows == 0 {
			return pkg.NewOrderExistsError().WithParam("order_id", order.OrderID)
		}

		for _, item := range order.Items {
			if _, err := tx.ExecContext(ctx, tx.Rebind(insertItem),
				item.OrderID, item.LineNo, item.SKU, item.Qty, item.UnitPriceCents, item.DiscountBPS,
			); err != nil {
				return pkg.NewDatabaseError(err)
			}
		}

		return nil
	})
}

func (r *OrderRepository) GetOrderByID(ctx context.Context, orderID string) (docs model.Order, err error) {
	query := fmt.Sprintf(`
	SELECT order_id, tenant_id, status, confirmed_at, confirmed_by_user_id,
	       currency, customer_phone, customer_external_ref
	FROM %s
	WHERE order_id = ?`, TableOrders)

	if err = r.DB.GetContext(ctx, &docs, r.DB.Rebind(query), orderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return docs, pkg.NewNotFoundError(pkg.MsgOrderNotFound, err).
				WithParam("order_id", orderID)
		}
		return docs, pkg.NewDatabaseError(err)
	}

	return docs, nil
}

// Ordering by line_no keeps chunking deterministic: the same order always splits the
// same way, so a retry after a partial dispatch lines up with what the ERP received.
func (r *OrderRepository) GetItemsByOrderID(ctx context.Context, orderID string) (docs []model.OrderItem, err error) {
	query := fmt.Sprintf(`
	SELECT order_id, line_no, sku, qty, unit_price_cents, discount_bps
	FROM %s
	WHERE order_id = ?
	ORDER BY line_no`, TableOrderItems)

	if err = r.DB.SelectContext(ctx, &docs, r.DB.Rebind(query), orderID); err != nil {
		return nil, pkg.NewDatabaseError(err)
	}

	return docs, nil
}
