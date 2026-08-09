package server

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"zolo-test-integration/internal/app/handler"
)

func Router(option handler.HandlerOptions, e *echo.Echo) {
	sync := handler.SyncHandler{HandlerOptions: option}
	order := handler.OrderHandler{HandlerOptions: option}

	v1 := e.Group("/api/v1")

	orders := v1.Group("/orders")
	orders.POST("", order.CreateOrder)
	orders.POST("/batch-sync", sync.BatchSyncOrders)
	orders.POST("/:id/sync", sync.SyncOrder)
	orders.GET("/:id/sync-status", sync.GetSyncStatus)

	e.GET("/healthz", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
}
