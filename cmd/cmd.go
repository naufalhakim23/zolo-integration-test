package cmd

import (
	"log/slog"
	"os"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/server"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/driver"
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

	sqliteConn, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{
		Path: config.Database.Path,
	})
	if err != nil {
		logger.Error("Failed to connect to database", "error", err)
		panic("Failed to connect to database")
	}
	defer sqliteConn.Close()

	options := &pkg.OptionsApplication{
		Config: config,
		DB:     sqliteConn,
		Logger: logger,
	}

	app := server.NewServer(options)
	app.ServerRun()
}
