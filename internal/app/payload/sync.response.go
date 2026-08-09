package payload

import (
	"errors"
	"time"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
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

	// Replayed marks a response served from a previous attempt rather than a
	// fresh ERP call, idempting the request.
	Replayed bool `json:"replayed,omitempty"`
}

type SyncLine struct {
	SKU      string `json:"sku"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

func LinesFromModel(lines []model.SyncLineResult) []SyncLine {
	if len(lines) == 0 {
		return nil
	}

	out := make([]SyncLine, 0, len(lines))
	for _, line := range lines {
		item := SyncLine{SKU: line.SKU, Accepted: line.Accepted}
		if line.Reason != nil {
			item.Reason = *line.Reason
		}
		out = append(out, item)
	}

	return out
}

// SyncResultFromError turns a per-order failure into a result row, so a batch
// response has one entry per requested.
func SyncResultFromError(orderID string, err error) SyncResult {
	result := SyncResult{
		OrderID:   orderID,
		Status:    pkg.StatusFailed,
		ErrorCode: pkg.CodeInternalError,
	}

	if appErr, ok := errors.AsType[*pkg.AppError](err); ok {
		result.ErrorCode = appErr.Code
		result.MessageCode = appErr.MessageCode
		result.Params = appErr.Params
	}

	return result
}

// Localize fills Message from the catalog. It runs at the HTTP edge, the only layer
// that knows what language the caller reads.
func (r *SyncResult) Localize(localizer *pkg.Localizer, lang string) {
	if r.MessageCode == "" {
		return
	}

	params := r.Params
	if params == nil {
		params = map[string]string{}
	}
	if _, ok := params["order_id"]; !ok && r.OrderID != "" {
		params["order_id"] = r.OrderID
	}

	r.Message = localizer.Translate(lang, r.MessageCode, params)
}
