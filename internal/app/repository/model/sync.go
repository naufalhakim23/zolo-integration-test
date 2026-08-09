package model

import (
	"strings"
	"time"
)

// SyncAttempt is the audit record for one (order, tenant) sync.
type SyncAttempt struct {
	ID               int64     `db:"id"`
	OrderID          string    `db:"order_id"`
	TenantID         string    `db:"tenant_id"`
	IdempotencyKey   string    `db:"idempotency_key"`
	Status           string    `db:"status"`
	ErrorCode        *string   `db:"error_code"`
	MessageCode      *string   `db:"message_code"`
	MessageParams    *string   `db:"message_params"`
	RequestSnapshot  *string   `db:"request_snapshot"`
	ResponseSnapshot *string   `db:"response_snapshot"`
	AttemptCount     int       `db:"attempt_count"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
	Lines            []SyncLineResult
}

type SyncLineResult struct {
	ID            int64   `db:"id"`
	SyncAttemptID int64   `db:"sync_attempt_id"`
	SKU           string  `db:"sku"`
	Accepted      bool    `db:"accepted"`
	Reason        *string `db:"reason"`
}

func (s SyncAttempt) Message() string {
	if s.MessageCode == nil || strings.TrimSpace(*s.MessageCode) == "" {
		return ""
	}
	return *s.MessageCode
}

// RejectedSKUs lists the line items the ERP refused, which is what the partial
// success message names back to the order taker.
func (s SyncAttempt) RejectedSKUs() []string {
	var out []string
	for _, line := range s.Lines {
		if !line.Accepted {
			out = append(out, line.SKU)
		}
	}
	return out
}
