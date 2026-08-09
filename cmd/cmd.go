package cmd

import (
	"log/slog"
	"os"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/server"
)

func Execute() {
	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("Starting application...")

	// Load configuration
	config, err := config.LoadConfiguration(".env")
	if err != nil {
		logger.Error("Failed to load configuration", "error", err)
		panic("Failed to load configuration")
	}

	app := server.NewServer(config, logger)
	app.ServerRun()
}
