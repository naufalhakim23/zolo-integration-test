package tenant

import (
	"time"

	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
)

type Mapper interface {
	TenantID() string

	// PreValidate rejects orders the ERP would refuse, before any network call
	PreValidate(order model.Order, now time.Time) *pkg.AppError

	// Build returns a slice so a tenant can chunk or not depending on its ERP's API
	Build(order model.Order) ([]erp.Request, *pkg.AppError)

	// Interpret treats a partial result as an outcome, not an error
	Interpret(order model.Order, responses []erp.Response) Outcome
}

// Outcome is the tenant-agnostic result the service persists and returns.
type Outcome struct {
	Status      string
	ErrorCode   string
	MessageCode string
	Params      map[string]string
	Lines       []model.SyncLineResult
}

func (o Outcome) IsFailure() bool {
	return o.Status == pkg.StatusFailed
}

// MaxOrderAge is a business rule about confirmation freshness.
const MaxOrderAge = 24 * time.Hour
