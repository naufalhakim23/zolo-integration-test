package handler

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/pkg"
)

type OrderHandler struct {
	HandlerOptions
}

// CreateOrder handles POST /api/v1/orders, the "Confirm Order" write from the dashboard.
// It is the only entry point where major-unit decimals are accepted.
func (h *OrderHandler) CreateOrder(c *echo.Context) error {
	req := new(payload.CreateOrderRequest)
	if err := c.Bind(req); err != nil {
		return h.respondError(c, pkg.NewBadRequestError(pkg.MsgInvalidRequest, err))
	}

	if errs := req.Validate(); len(errs) > 0 {
		appErr := pkg.NewValidationError(pkg.MsgInvalidRequest, nil)
		appErr.Details = errs
		return h.respondError(c, appErr)
	}

	order, err := h.Service.Order.CreateOrder(c.Request().Context(), *req)
	if err != nil {
		return h.respondError(c, err)
	}

	return c.JSON(http.StatusCreated, payload.BaseResponse{
		Status:  http.StatusCreated,
		Message: h.Localizer.Translate(language(c), pkg.MsgOrderCreated, map[string]string{"order_id": order.OrderID}),
		Data:    payload.CreatedOrderFromModel(order),
	})
}
