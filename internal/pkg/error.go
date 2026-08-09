package pkg

import "net/http"

// AppError is the single error shape crossing layer boundaries. Code is stable and
// machine-readable
type AppError struct {
	Code        string            `json:"code"`
	MessageCode string            `json:"-"` // i18n template key, e.g. "sync.order_not_found"
	Params      map[string]string `json:"-"`
	Details     []string          `json:"details,omitempty"` // optional, e.g. ["line 1: SKU 1234 rejected", "line 2: SKU 5678 rejected"]
	StatusCode  int               `json:"-"`
	Err         error             `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewError(code, messageCode string, statusCode int, err error) *AppError {
	return &AppError{
		Code:        code,
		MessageCode: messageCode,
		StatusCode:  statusCode,
		Err:         err,
	}
}

// WithParam attaches a substitution value for the i18n template, e.g. the SKU
// that ERP B rejected.
func (e *AppError) WithParam(key, value string) *AppError {
	if e.Params == nil {
		e.Params = map[string]string{}
	}
	e.Params[key] = value
	return e
}

func NewValidationError(messageCode string, err error) *AppError {
	return NewError(CodeValidationError, messageCode, http.StatusUnprocessableEntity, err)
}

func NewNotFoundError(messageCode string, err error) *AppError {
	return NewError(CodeNotFound, messageCode, http.StatusNotFound, err)
}

func NewBadRequestError(messageCode string, err error) *AppError {
	return NewError(CodeBadRequest, messageCode, http.StatusBadRequest, err)
}

func NewConflictError(messageCode string, err error) *AppError {
	return NewError(CodeSyncInProgress, messageCode, http.StatusConflict, err)
}

func NewDatabaseError(err error) *AppError {
	return NewError(CodeInternalError, MsgInternalError, http.StatusInternalServerError, err)
}

func NewUpstreamError(messageCode string, err error) *AppError {
	return NewError(CodeERPUnavailable, messageCode, http.StatusBadGateway, err)
}
