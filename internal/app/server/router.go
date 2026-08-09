package server

import "github.com/labstack/echo/v5"

func Router(e *echo.Echo) {
	// Orders
	v1 := e.Group("/api/v1")
	v1.GET("/orders", GetOrders)
}

// Dummy handler for fetching orders
func GetOrders(c *echo.Context) error {
	// Placeholder for fetching orders logic
	orders := []string{"Order1", "Order2", "Order3"}
	return c.JSON(200, orders)
}
