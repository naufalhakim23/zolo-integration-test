package handler

import (
	"errors"
	"net/http"
	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/pkg"

	"github.com/labstack/echo/v5"
)

type HandlerOptions struct {
	pkg.OptionsApplication
	*service.Service
}

func (h *HandlerOptions) respondError(c *echo.Context, err error) error {
	var appErr *pkg.AppError
	if !errors.As(err, &appErr) {
		h.Logger.Error("unhandled error", "path", c.Request().URL.Path, "error", err)
		appErr = pkg.NewError(pkg.CodeInternalError, pkg.MsgInternalError, http.StatusInternalServerError, err)
	}

	return c.JSON(appErr.StatusCode, payload.BaseResponse{
		Status:  appErr.StatusCode,
		Message: appErr.MessageCode,
		Error: payload.ErrorDetail{
			Code: appErr.Code,
		},
	})
}
