package service

import (
	"context"

	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/app/repository/model"
)

type (
	IOrderService interface {
		CreateOrder(ctx context.Context, req payload.CreateOrderRequest) (model.Order, error)
	}

	OrderService struct {
		ServiceOption
	}
)

func InitiateOrderService(opt ServiceOption) IOrderService {
	return &OrderService{ServiceOption: opt}
}

// CreateOrder stores a confirmed order from the dashboard. Rejecting an unmapped tenant
// here means an order can never be stored in a state where no sync could ever succeed.
func (s *OrderService) CreateOrder(ctx context.Context, req payload.CreateOrderRequest) (model.Order, error) {
	if _, appErr := s.Registry.Get(req.TenantID); appErr != nil {
		return model.Order{}, appErr
	}

	order := req.ToModel()
	if err := s.Repository.Order.CreateOrder(ctx, order); err != nil {
		return model.Order{}, err
	}

	s.Logger.Info("order confirmed",
		"order_id", order.OrderID,
		"tenant_id", order.TenantID,
		"lines", len(order.Items),
	)

	return order, nil
}
