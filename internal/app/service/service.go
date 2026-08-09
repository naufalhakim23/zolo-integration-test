package service

import (
	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
)

type ServiceOption struct {
	pkg.OptionsApplication
	Repository *repository.Repository
	Registry   *tenant.Registry
	ERPClient  *erp.Client
}

type Service struct {
	Sync  ISyncService
	Order IOrderService
}
