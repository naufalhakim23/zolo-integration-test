package payload

import (
	"time"
)

// SyncResult is the audit output returned to the dashboard.
type SyncResult struct {
	OrderID      string            `json:"order_id"`
	TenantID     string            `json:"tenant_id,omitempty"`
	Status       string            `json:"status"`
	Message      string            `json:"message,omitempty"`
	MessageCode  string            `json:"message_code,omitempty"`
	Params       map[string]string `json:"-"`
	ErrorCode    string            `json:"error_code,omitempty"`
	Lines        []SyncLine        `json:"lines,omitempty"`
	AttemptCount int               `json:"attempt_count,omitempty"`
	SyncedAt     time.Time         `json:"synced_at,omitzero"`

	Replayed bool `json:"replayed,omitempty"`
}

type SyncLine struct {
	SKU      string `json:"sku"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}
