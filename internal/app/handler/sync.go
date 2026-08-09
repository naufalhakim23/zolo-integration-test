package handler

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/pkg"
)

type SyncHandler struct {
	HandlerOptions
}

// SyncOrder handles POST /api/v1/orders/:id/sync
func (h *SyncHandler) SyncOrder(c *echo.Context) error {
	orderID := c.Param("id")
	if orderID == "" {
		return h.respondError(c, pkg.NewBadRequestError(pkg.MsgInvalidRequest, nil))
	}

	result, err := h.Service.Sync.SyncOrder(c.Request().Context(), orderID)
	if err != nil {
		return h.respondError(c, err)
	}

	result.Localize(h.Localizer, language(c))

	// A partial success answers 207, so the dashboard can tell "everything landed" from
	// "some lines need attention" without inspecting the body.
	status := service.HTTPStatus(result.Status)

	return c.JSON(status, payload.BaseResponse{
		Status:  status,
		Message: result.Message,
		Data:    result,
	})
}

// BatchSyncOrders handles POST /api/v1/orders/batch-sync.
func (h *SyncHandler) BatchSyncOrders(c *echo.Context) error {
	req := new(payload.BatchSyncRequest)
	if err := c.Bind(req); err != nil {
		return h.respondError(c, pkg.NewBadRequestError(pkg.MsgInvalidRequest, err))
	}

	if errs := req.Validate(); len(errs) > 0 {
		appErr := pkg.NewValidationError(pkg.MsgInvalidRequest, nil)
		appErr.Details = errs
		return h.respondError(c, appErr)
	}

	results, err := h.Service.Sync.BatchSyncOrders(c.Request().Context(), req.OrderIDs)
	if err != nil {
		return h.respondError(c, err)
	}

	lang := language(c)
	for i := range results {
		results[i].Localize(h.Localizer, lang)
	}

	return c.JSON(http.StatusOK, payload.BaseResponse{
		Status:  http.StatusOK,
		Message: "batch sync completed",
		Data:    results,
	})
}

// GetSyncStatus handles GET /api/v1/orders/:id/sync-status, the audit read.
func (h *SyncHandler) GetSyncStatus(c *echo.Context) error {
	orderID := c.Param("id")
	if orderID == "" {
		return h.respondError(c, pkg.NewBadRequestError(pkg.MsgInvalidRequest, nil))
	}

	result, err := h.Service.Sync.GetSyncStatus(c.Request().Context(), orderID)
	if err != nil {
		return h.respondError(c, err)
	}

	result.Localize(h.Localizer, language(c))

	return c.JSON(http.StatusOK, payload.BaseResponse{
		Status:  http.StatusOK,
		Message: result.Message,
		Data:    result,
	})
}
