package server

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"zolo-test-integration/internal/app/handler"
)

func Router(option handler.HandlerOptions, e *echo.Echo) {
	// Orders
	v1 := e.Group("/api/v1")
	v1.GET("/orders", GetOrders)

	e.GET("/healthz", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
}

// Dummy handler for fetching orders
func GetOrders(c *echo.Context) error {
	// Placeholder for fetching orders logic
	orders := []string{"Order1", "Order2", "Order3"}
	return c.JSON(200, orders)
}
