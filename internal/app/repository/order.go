package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
)

type (
	IOrderRepository interface {
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
