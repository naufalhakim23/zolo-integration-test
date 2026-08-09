package server

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/pkg"
)

type IServer interface {
	ServerRun()
}

type Server struct {
	config *config.Config
	logger *slog.Logger
}

func NewServer(options *pkg.OptionsApplication) IServer {
	return &Server{
		logger: options.Logger,
		config: options.Config,
	}
}

func (s *Server) ServerRun() {
	s.logger.Info("Server is running...")

	e := echo.New()
	e.Use(middleware.Recover())

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		// For simplicity, we allow all origins and headers. Adjust as needed for production.
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
	}))

	if err := e.Start(":" + s.config.Application.Port); err != nil {
		s.logger.Error("Failed to start server", "error", err)
	}
}
