package tenant

import (
	"zolo-test-integration/internal/pkg"
)

// Registry resolves a tenant_id to its mapper. Built once at startup and only read
type Registry struct {
	mappers map[string]Mapper
}

func NewRegistry() *Registry {
	return &Registry{mappers: map[string]Mapper{}}
}

func (r *Registry) Register(m Mapper) {
	r.mappers[m.TenantID()] = m
}

// Get names the tenant, if not registered will return an error.
func (r *Registry) Get(tenantID string) (Mapper, *pkg.AppError) {
	m, ok := r.mappers[tenantID]
	if !ok {
		return nil, pkg.NewError(pkg.CodeUnknownTenant, pkg.MsgUnknownTenant, 422, nil).
			WithParam("tenant_id", tenantID)
	}
	return m, nil
}

func (r *Registry) TenantIDs() []string {
	out := make([]string, 0, len(r.mappers))
	for id := range r.mappers {
		out = append(out, id)
	}
	return out
}
