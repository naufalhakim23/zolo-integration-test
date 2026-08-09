package server

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"zolo-test-integration/internal/app/handler"
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/pkg"
)

type IServer interface {
	ServerRun()
	Handler() *echo.Echo
}

type Server struct {
	option pkg.OptionsApplication
	svc    *service.Service
}

func NewServer(opt pkg.OptionsApplication, svc *service.Service) IServer {
	return &Server{option: opt, svc: svc}
}

// Handler builds the routed Echo instance.
func (s *Server) Handler() *echo.Echo {
	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, "Accept-Language"},
	}))

	Router(handler.HandlerOptions{
		OptionsApplication: s.option,
		Service:            s.svc,
	}, e)

	return e
}

// Using a custom HTTP server for graceful shutdown.
func (s *Server) ServerRun() {
	address := ":" + s.option.Config.Application.Port
	httpServer := &http.Server{
		Addr:    address,
		Handler: s.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		s.option.Logger.Info("server listening", "address", address, "env", s.option.Config.Application.Env)

		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.option.Logger.Error("failed to start server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		s.option.Logger.Error("shutdown failed", "error", err)
		return
	}

	s.option.Logger.Info("server shut down gracefully")
}
