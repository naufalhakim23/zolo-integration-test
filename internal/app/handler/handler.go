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

// language reads the caller's preferred language for the message catalog.
func language(c *echo.Context) string {
	return c.Request().Header.Get("Accept-Language")
}

// respondError renders an error with its translated message. An unrecognised error
// collapses to a generic internal message.
func (h *HandlerOptions) respondError(c *echo.Context, err error) error {
	var appErr *pkg.AppError
	if !errors.As(err, &appErr) {
		h.Logger.Error("unhandled error", "path", c.Request().URL.Path, "error", err)
		appErr = pkg.NewError(pkg.CodeInternalError, pkg.MsgInternalError, http.StatusInternalServerError, err)
	}

	return c.JSON(appErr.StatusCode, payload.BaseResponse{
		Status:  appErr.StatusCode,
		Message: h.Localizer.Translate(language(c), appErr.MessageCode, appErr.Params),
		Error: payload.ErrorDetail{
			Code:    appErr.Code,
			Details: appErr.Details,
		},
	})
}
