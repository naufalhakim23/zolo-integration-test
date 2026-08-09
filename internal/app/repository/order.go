package repository

import (
	"context"
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
	SELECT * 
	FROM %s 
	WHERE id = %s`, TableOrders, r.DB.Rebind("?"))

	if err := r.DB.GetContext(ctx, &docs, query, orderID); err != nil {
		return docs, pkg.NewNotFoundError(pkg.MsgOrderNotFound, err).
			WithParam("order_id", orderID)
	}

	return docs, nil
}

func (r *OrderRepository) GetItemsByOrderID(ctx context.Context, orderID string) (docs []model.OrderItem, err error) {
	query := fmt.Sprintf(`
	SELECT * 
	FROM %s 
	WHERE 
	order_id = %s 
	ORDER BY line_no`, TableOrderItems, r.DB.Rebind("?"))

	if err := r.DB.SelectContext(ctx, &docs, query, orderID); err != nil {
		return docs, pkg.NewNotFoundError(pkg.MsgOrderNotFound, err).
			WithParam("order_id", orderID)
	}

	return docs, nil
}
