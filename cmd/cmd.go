package cmd

import (
	"log/slog"
	"os"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository"
	"zolo-test-integration/internal/app/server"
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/driver"
)

func Execute() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.LoadConfiguration(".env")
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger = logger.With("app", cfg.Application.Name, "env", cfg.Application.Env)

	db, err := driver.NewSQLiteDatabaseDriver(driver.SQLiteOption{Path: cfg.Database.Path})
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	options := pkg.OptionsApplication{
		Config: cfg,
		DB:     db,
		Logger: logger,
	}

	repo := repositoryConnector(repository.RepositoryOption{OptionsApplication: options})

	svc := serviceConnector(service.ServiceOption{
		OptionsApplication: options,
		Repository:         repo,
		Registry:           tenant.DefaultRegistry(cfg.ERP),
		ERPClient:          erp.NewClient(cfg.ERP, logger),
	})

	server.NewServer(options, svc).ServerRun()
}

func repositoryConnector(opt repository.RepositoryOption) *repository.Repository {
	return &repository.Repository{
		Order: repository.InitiateOrderRepository(opt),
		Sync:  repository.InitiateSyncRepository(opt),
	}
}

func serviceConnector(opt service.ServiceOption) *service.Service {
	return &service.Service{
		Sync: service.InitiateSyncService(opt),
	}
}
