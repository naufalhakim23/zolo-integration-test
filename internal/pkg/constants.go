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

// Machine-readable error codes returned to the dashboard.
const (
	CodeValidationError = "VALIDATION_ERROR"
	CodeBadRequest      = "BAD_REQUEST"
	CodeNotFound        = "NOT_FOUND"
	CodeInternalError   = "INTERNAL_ERROR"
)

// Message codes
const (
	MsgInternalError     = "sync.internal_error"
	MsgOrderNotFound     = "sync.order_not_found"
	MsgInvalidRequest    = "sync.invalid_request"
	MsgOrderNotConfirmed = "sync.order_not_confirmed"
)
