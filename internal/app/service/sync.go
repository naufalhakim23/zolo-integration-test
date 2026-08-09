package service

import (
	"context"
	"net/http"
	"time"

	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
)

type (
	ISyncService interface {
		SyncOrder(ctx context.Context, orderID string) (payload.SyncResult, error)
		BatchSyncOrders(ctx context.Context, orderIDs []string) ([]payload.SyncResult, error)
		GetSyncStatus(ctx context.Context, orderID string) (payload.SyncResult, error)
	}

	SyncService struct {
		ServiceOption

		now func() time.Time // injectable expiration
	}
)

func InitiateSyncService(opt ServiceOption) ISyncService {
	return &SyncService{ServiceOption: opt, now: time.Now}
}

// SyncOrder runs the on-demand push for one order.
// fetch -> resolve mapper -> pre-validate -> claim -> build -> dispatch -> interpret -> persist
func (s *SyncService) SyncOrder(ctx context.Context, orderID string) (payload.SyncResult, error) {
	order, err := s.loadOrder(ctx, orderID)
	if err != nil {
		return payload.SyncResult{}, err
	}

	mapper, appErr := s.Registry.Get(order.TenantID)
	if appErr != nil {
		return payload.SyncResult{}, appErr
	}

	// Pre-validation runs before the claim so a permanently unsendable order never
	// occupies an idempotency key and never reaches the network.
	if appErr := mapper.PreValidate(order, s.now()); appErr != nil {
		s.Logger.Warn("order rejected before dispatch",
			"order_id", order.OrderID,
			"tenant_id", order.TenantID,
			"code", appErr.Code,
		)
		return payload.SyncResult{}, appErr
	}

	key := model.IdempotencyKey(order.OrderID, order.TenantID)

	attempt, claimed, err := s.Repository.Sync.ClaimAttempt(ctx, order.OrderID, order.TenantID, key)
	if err != nil {
		return payload.SyncResult{}, err
	}

	// Someone else owns this sync. A double-click lands here and replays the first
	// result instead of pushing the order to the ERP twice.
	if !claimed {
		return s.replay(order, attempt)
	}

	requests, appErr := mapper.Build(order)
	if appErr != nil {
		return s.finish(ctx, order, attempt, tenant.Outcome{
			Status:      pkg.StatusFailed,
			ErrorCode:   appErr.Code,
			MessageCode: appErr.MessageCode,
			Params:      appErr.Params,
		}, nil, nil)
	}

	responses := s.ERPClient.Dispatch(ctx, requests)
	outcome := mapper.Interpret(order, responses)

	s.Logger.Info("order sync completed",
		"order_id", order.OrderID,
		"tenant_id", order.TenantID,
		"status", outcome.Status,
		"requests", len(requests),
		"idempotency_key", key,
	)

	return s.finish(ctx, order, attempt, outcome, requests, responses)
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

	return payload.SyncResult{
		OrderID:      attempt.OrderID,
		TenantID:     attempt.TenantID,
		Status:       attempt.Status,
		ErrorCode:    derefString(attempt.ErrorCode),
		MessageCode:  derefString(attempt.MessageCode),
		Params:       decodeParams(attempt.MessageParams),
		Lines:        payload.LinesFromModel(attempt.Lines),
		AttemptCount: attempt.AttemptCount,
		SyncedAt:     attempt.UpdatedAt,
	}, nil
}

// replay returns the state owned by another request without dispatching.
func (s *SyncService) replay(order model.Order, attempt model.SyncAttempt) (payload.SyncResult, error) {
	if !pkg.IsTerminal(attempt.Status) {
		return payload.SyncResult{}, pkg.NewConflictError(pkg.MsgSyncInProgress, nil).
			WithParam("order_id", order.OrderID)
	}

	return payload.SyncResult{
		OrderID:      attempt.OrderID,
		TenantID:     attempt.TenantID,
		Status:       attempt.Status,
		ErrorCode:    derefString(attempt.ErrorCode),
		MessageCode:  derefString(attempt.MessageCode),
		Params:       decodeParams(attempt.MessageParams),
		Lines:        payload.LinesFromModel(attempt.Lines),
		AttemptCount: attempt.AttemptCount,
		SyncedAt:     attempt.UpdatedAt,
		Replayed:     true,
	}, nil
}

// finish writes the terminal state plus the request and response snapshots. The
// snapshots are what make an incident debuggable: when a tenant says an order arrived
// wrong, the exact bytes sent and received are already recorded against the attempt.
func (s *SyncService) finish(
	ctx context.Context,
	order model.Order,
	attempt model.SyncAttempt,
	outcome tenant.Outcome,
	requests []erp.Request,
	responses []erp.Response,
) (payload.SyncResult, error) {
	attempt.Status = outcome.Status
	attempt.ErrorCode = nullable(outcome.ErrorCode)
	attempt.MessageCode = nullable(outcome.MessageCode)
	attempt.MessageParams = nullable(encodeJSON(outcome.Params))
	attempt.RequestSnapshot = nullable(encodeJSON(requests))
	attempt.ResponseSnapshot = nullable(encodeJSON(snapshotResponses(responses)))
	attempt.Lines = outcome.Lines

	if err := s.Repository.Sync.CompleteAttempt(ctx, attempt); err != nil {
		return payload.SyncResult{}, err
	}

	return payload.SyncResult{
		OrderID:      order.OrderID,
		TenantID:     order.TenantID,
		Status:       outcome.Status,
		ErrorCode:    outcome.ErrorCode,
		MessageCode:  outcome.MessageCode,
		Params:       outcome.Params,
		Lines:        payload.LinesFromModel(outcome.Lines),
		AttemptCount: attempt.AttemptCount,
		SyncedAt:     s.now().UTC(),
	}, nil
}

// HTTPStatus maps a sync status to its response code. A partial success is 207 so the
// caller can branch on the status line alone.
func HTTPStatus(status string) int {
	switch status {
	case pkg.StatusSynced:
		return http.StatusOK
	case pkg.StatusPartialSuccess:
		return http.StatusMultiStatus
	default:
		return http.StatusBadGateway
	}
}
