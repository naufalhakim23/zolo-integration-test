package handler

import (
	"zolo-test-integration/internal/app/service"
	"zolo-test-integration/internal/pkg"
)

type HandlerOptions struct {
	pkg.OptionsApplication
	*service.Service
}
