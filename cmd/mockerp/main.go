package main

import (
	"log/slog"
	"net/http"
	"os"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/mockerp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("app", "mock-erp")

	address := ":" + config.GetEnv("MOCK_ERP_PORT", "9090")
	logger.Info("mock erp listening", "address", address)

	if err := http.ListenAndServe(address, mockerp.NewServer(logger).Handler()); err != nil {
		logger.Error("mock erp stopped", "error", err)
		os.Exit(1)
	}
}
