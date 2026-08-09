package payload

import (
	"fmt"
	"strings"
)

// MaxBatchSize caps one batch request. Each order is a separate ERP round trip, so an
// unbounded list would hold a connection open for minutes.
const MaxBatchSize = 100

// BatchSyncRequest request body for POST /api/v1/orders/batch-sync
type BatchSyncRequest struct {
	OrderIDs []string `json:"order_ids"`
}

// Validate returns one message per problem, named by JSON field so the dashboard can
// show them as-is.
func (r *BatchSyncRequest) Validate() []string {
	var errs []string

	switch {
	case len(r.OrderIDs) == 0:
		errs = append(errs, "order_ids is required")
	case len(r.OrderIDs) > MaxBatchSize:
		errs = append(errs, fmt.Sprintf("order_ids must contain at most %d entries", MaxBatchSize))
	}

	for i, id := range r.OrderIDs {
		if strings.TrimSpace(id) == "" {
			errs = append(errs, fmt.Sprintf("order_ids[%d] cannot be blank", i))
		}
	}

	return errs
}
