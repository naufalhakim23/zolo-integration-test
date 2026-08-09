package service

import (
	"context"
	"encoding/json"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
)

func (s *SyncService) loadOrder(ctx context.Context, orderID string) (order model.Order, err error) {
	order, err = s.Repository.Order.GetOrderByID(ctx, orderID)
	if err != nil {
		return order, err
	}

	if order.Status != pkg.OrderStatusConfirmed {
		return model.Order{}, pkg.NewValidationError(pkg.MsgOrderNotConfirmed, nil).
			WithParam("order_id", orderID)
	}

	order.Items, err = s.Repository.Order.GetItemsByOrderID(ctx, orderID)
	if err != nil {
		return model.Order{}, err
	}

	return order, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func decodeParams(raw *string) map[string]string {
	if raw == nil || *raw == "" {
		return nil
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(*raw), &params); err != nil {
		return nil
	}
	return params
}
