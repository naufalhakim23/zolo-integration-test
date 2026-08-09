package tenant_test

import (
	"testing"
	"time"

	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/app/tenant"
	"zolo-test-integration/internal/pkg"
)

type stubMapper struct{ id string }

func (s stubMapper) TenantID() string { return s.id }

func (s stubMapper) PreValidate(model.Order, time.Time) *pkg.AppError { return nil }

func (s stubMapper) Build(model.Order) ([]erp.Request, *pkg.AppError) { return nil, nil }

func (s stubMapper) Interpret(model.Order, []erp.Response) tenant.Outcome {
	return tenant.Outcome{Status: pkg.StatusSynced}
}

func TestRegistryGet(t *testing.T) {
	cases := []struct {
		name       string
		registered []string
		lookup     string
		wantCode   string
	}{
		{name: "registered tenant", registered: []string{pkg.TenantAlpha}, lookup: pkg.TenantAlpha},
		{name: "second tenant", registered: []string{pkg.TenantAlpha, pkg.TenantBeta}, lookup: pkg.TenantBeta},
		{name: "unknown tenant", registered: []string{pkg.TenantAlpha}, lookup: "tenant_gamma", wantCode: pkg.CodeUnknownTenant},
		{name: "empty registry", lookup: pkg.TenantAlpha, wantCode: pkg.CodeUnknownTenant},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := tenant.NewRegistry()
			for _, id := range c.registered {
				r.Register(stubMapper{id: id})
			}

			m, appErr := r.Get(c.lookup)

			if c.wantCode != "" {
				if appErr == nil {
					t.Fatalf("Get(%q) = %v, want error %s", c.lookup, m, c.wantCode)
				}
				if appErr.Code != c.wantCode {
					t.Errorf("code = %s, want %s", appErr.Code, c.wantCode)
				}
				if appErr.Params["tenant_id"] != c.lookup {
					t.Errorf("params[tenant_id] = %q, want %q", appErr.Params["tenant_id"], c.lookup)
				}
				return
			}

			if appErr != nil {
				t.Fatalf("Get(%q): unexpected error %v", c.lookup, appErr)
			}
			if m.TenantID() != c.lookup {
				t.Errorf("mapper = %q, want %q", m.TenantID(), c.lookup)
			}
		})
	}
}

// Re-registering a tenant replaces it rather than growing the map, so a bad wiring
// order cannot leave two mappers claiming the same tenant.
func TestRegistryRegisterIsIdempotent(t *testing.T) {
	cases := []struct {
		name     string
		register []string
		wantIDs  int
	}{
		{name: "distinct tenants", register: []string{pkg.TenantAlpha, pkg.TenantBeta}, wantIDs: 2},
		{name: "duplicate tenant", register: []string{pkg.TenantAlpha, pkg.TenantAlpha}, wantIDs: 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := tenant.NewRegistry()
			for _, id := range c.register {
				r.Register(stubMapper{id: id})
			}
			if got := len(r.TenantIDs()); got != c.wantIDs {
				t.Errorf("TenantIDs() = %d entries, want %d", got, c.wantIDs)
			}
		})
	}
}

func TestOutcomeIsFailure(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "failed", status: pkg.StatusFailed, want: true},
		{name: "synced", status: pkg.StatusSynced},
		{name: "partial success is not a failure", status: pkg.StatusPartialSuccess},
		{name: "pending", status: pkg.StatusPending},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (tenant.Outcome{Status: c.status}).IsFailure(); got != c.want {
				t.Errorf("Outcome{%s}.IsFailure() = %v, want %v", c.status, got, c.want)
			}
		})
	}
}
