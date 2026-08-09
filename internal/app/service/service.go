package service

import (
	"zolo-test-integration/internal/app/repository"
	"zolo-test-integration/internal/pkg"
)

type ServiceOption struct {
	pkg.OptionsApplication
	Repository *repository.Repository
}

type Service struct {
}
