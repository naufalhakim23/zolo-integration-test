package handler

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"zolo-test-integration/internal/app/payload"
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

	return c.JSON(http.StatusOK, payload.BaseResponse{
		Status:  http.StatusOK,
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

	results, err := h.Service.Sync.BatchSyncOrders(c.Request().Context(), req.OrderIDs)
	if err != nil {
		return h.respondError(c, err)
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

	return c.JSON(http.StatusOK, payload.BaseResponse{
		Status:  http.StatusOK,
		Message: result.Message,
		Data:    result,
	})
}
