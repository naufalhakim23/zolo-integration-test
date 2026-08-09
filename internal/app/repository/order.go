package repository

import (
	"context"

	"zolo-test-integration/internal/app/repository/model"
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

	return docs, nil
}

func (r *OrderRepository) GetItemsByOrderID(ctx context.Context, orderID string) (docs []model.OrderItem, err error) {

	return docs, nil
}
