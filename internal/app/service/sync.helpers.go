package service

import (
	"context"
	"encoding/json"

	"zolo-test-integration/internal/app/erp"
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

// responseSnapshot is the audit projection of an ERP reply. erp.Response is not
// serialised directly because its TransportErr is an interface that marshals to an
// empty object, erasing the most useful field exactly when dispatch failed.
type responseSnapshot struct {
	Path       string          `json:"path"`
	ChunkIndex int             `json:"chunk_index"`
	SKUs       []string        `json:"skus"`
	StatusCode int             `json:"status_code"`
	Attempts   int             `json:"attempts"`
	Body       json.RawMessage `json:"body,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func snapshotResponses(responses []erp.Response) []responseSnapshot {
	if len(responses) == 0 {
		return nil
	}

	out := make([]responseSnapshot, 0, len(responses))
	for _, resp := range responses {
		snap := responseSnapshot{
			Path:       resp.Request.Path,
			ChunkIndex: resp.Request.ChunkIndex,
			SKUs:       resp.Request.SKUs,
			StatusCode: resp.StatusCode,
			Attempts:   resp.Attempts,
			Body:       resp.Body,
		}
		if resp.TransportErr != nil {
			snap.Error = resp.TransportErr.Error()
		}
		out = append(out, snap)
	}

	return out
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func encodeJSON(v any) string {
	if v == nil {
		return ""
	}
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
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
