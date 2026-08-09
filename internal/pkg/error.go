package pkg

import "net/http"

type AppError struct {
	Code        string `json:"code"`
	MessageCode string `json:"-"`
	StatusCode  int    `json:"-"`
	Err         error  `json:"-"`
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

func NewBadRequestError(messageCode string, err error) *AppError {
	return NewError(CodeBadRequest, messageCode, http.StatusBadRequest, err)
}

func NewDatabaseError(err error) *AppError {
	return NewError(CodeInternalError, MsgInternalError, http.StatusInternalServerError, err)
}
