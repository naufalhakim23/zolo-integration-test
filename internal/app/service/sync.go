package service

import (
	"context"

	"zolo-test-integration/internal/app/payload"
)

type (
	ISyncService interface {
		SyncOrder(ctx context.Context, orderID string) (payload.SyncResult, error)
		BatchSyncOrders(ctx context.Context, orderIDs []string) ([]payload.SyncResult, error)
		GetSyncStatus(ctx context.Context, orderID string) (payload.SyncResult, error)
	}

	SyncService struct {
		ServiceOption
	}
)

func InitiateSyncService(opt ServiceOption) ISyncService {
	return &SyncService{ServiceOption: opt}
}

// SyncOrder runs the on-demand push for one order.
func (s *SyncService) SyncOrder(ctx context.Context, orderID string) (payload.SyncResult, error) {
	// 1. Get the order and its items, validating that it is in a confirmed state.
	order, err := s.loadOrder(ctx, orderID)
	if err != nil {
		return payload.SyncResult{}, err
	}
	s.Logger.Info("syncing order", "order_id", orderID, "item_count", len(order.Items))

	// 2. Get the tenant id from the order and use it to get the ERP client for this tenant.

	// 3. Validate the order and items against the ERP client.

	// 4. Create a sync attempt record in the database with status PENDING.

	// 5. Call the ERP client to push the order and items.

	// 6. Update the sync attempt record with the result of the push.

	// 7. Return the result of the sync attempt.
	return payload.SyncResult{
		// OrderID: order,
		// Status:  payload.SyncStatusPending,
	}, nil
}

// BatchSyncOrders syncs several orders and always returns one result per input.
func (s *SyncService) BatchSyncOrders(ctx context.Context, orderIDs []string) ([]payload.SyncResult, error) {
	results := make([]payload.SyncResult, 0, len(orderIDs))

	for _, orderID := range orderIDs {
		result, err := s.SyncOrder(ctx, orderID)
		if err != nil {
			results = append(results, payload.SyncResultFromError(orderID, err))
			continue
		}
		results = append(results, result)
	}

	return results, nil
}

// GetSyncStatus returns the last known state of a sync attempt
func (s *SyncService) GetSyncStatus(ctx context.Context, orderID string) (payload.SyncResult, error) {
	attempt, err := s.Repository.Sync.GetLatestAttemptByOrderID(ctx, orderID)
	if err != nil {
		return payload.SyncResult{}, err
	}
	payload := payload.SyncResult{
		OrderID:      attempt.OrderID,
		TenantID:     attempt.TenantID,
		Status:       attempt.Status,
		ErrorCode:    derefString(attempt.ErrorCode),
		MessageCode:  derefString(attempt.MessageCode),
		Params:       decodeParams(attempt.MessageParams),
		Lines:        payload.LinesFromModel(attempt.Lines),
		AttemptCount: attempt.AttemptCount,
		SyncedAt:     attempt.UpdatedAt,
	}

	return payload, nil
}
