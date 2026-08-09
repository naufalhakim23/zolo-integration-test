package pkg

import "net/http"

type AppError struct {
	Code        string            `json:"code"`
	MessageCode string            `json:"-"`
	Params      map[string]string `json:"-"`
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

func NewDatabaseError(err error) *AppError {
	return NewError(CodeInternalError, MsgInternalError, http.StatusInternalServerError, err)
}
