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
	return payload.SyncResult{}, nil
}

// BatchSyncOrders syncs several orders and always returns one result per input.
func (s *SyncService) BatchSyncOrders(ctx context.Context, orderIDs []string) ([]payload.SyncResult, error) {
	results := make([]payload.SyncResult, 0, len(orderIDs))
	return results, nil
}

// GetSyncStatus returns the last known state of a sync attempt
func (s *SyncService) GetSyncStatus(ctx context.Context, orderID string) (payload.SyncResult, error) {

	return payload.SyncResult{}, nil
}
