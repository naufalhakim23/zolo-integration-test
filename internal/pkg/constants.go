package pkg

// OrderStatus represents the status of an order in the system.
const (
	OrderStatusConfirmed = "CONFIRMED"
)

// Status Sync lifecycle states persisted in sync_attempts.status.
// with status PENDING is the claim marker written before the first ERP call.
const (
	StatusPending        = "PENDING"
	StatusSynced         = "SYNCED"
	StatusPartialSuccess = "PARTIAL_SUCCESS"
	StatusFailed         = "FAILED"
)

// IsTerminal reports whether an attempt has finished, so its stored result can
// be replayed to a repeat caller instead of dispatching again.
func IsTerminal(status string) bool {
	switch status {
	case StatusSynced, StatusPartialSuccess, StatusFailed:
		return true
	}
	return false
}

// Machine-readable error codes returned to the dashboard.
const (
	CodeValidationError = "VALIDATION_ERROR"
	CodeBadRequest      = "BAD_REQUEST"
	CodeNotFound        = "NOT_FOUND"
	CodeSyncInProgress  = "SYNC_IN_PROGRESS"
	CodeOrderExpired    = "ORDER_EXPIRED"
	CodeUnknownTenant   = "UNKNOWN_TENANT"
	CodeERPRejected     = "ERP_REJECTED"
	CodeERPUnavailable  = "ERP_UNAVAILABLE"
	CodeInternalError   = "INTERNAL_ERROR"
)

// i18n catalog keys.
const (
	MsgSyncSuccess       = "sync.success"
	MsgSyncPartial       = "sync.partial"
	MsgSyncFailed        = "sync.failed"
	MsgSyncInProgress    = "sync.in_progress"
	MsgOrderNotFound     = "sync.order_not_found"
	MsgOrderNotConfirmed = "sync.order_not_confirmed"
	MsgOrderExpired      = "sync.order_expired"
	MsgUnknownTenant     = "sync.unknown_tenant"
	MsgInvalidPartnerRef = "sync.invalid_partner_ref"
	MsgNoLineItems       = "sync.no_line_items"
	MsgLineRejected      = "sync.line_rejected"
	MsgERPUnavailable    = "sync.erp_unavailable"
	MsgERPRejected       = "sync.erp_rejected"
	MsgInternalError     = "sync.internal_error"
	MsgInvalidRequest    = "sync.invalid_request"
)

// Tenant identifiers the registry is keyed by.
const (
	TenantAlpha = "tenant_alpha"
	TenantBeta  = "tenant_beta"
)

// HeaderIdempotencyKey carries SHA256(order_id + tenant_id) to ERP A.
const (
	HeaderIdempotencyKey = "X-Idempotency-Key"
	HeaderChunkIndex     = "X-Chunk-Index"
)
