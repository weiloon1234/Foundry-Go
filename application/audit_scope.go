package application

import (
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var AuditScopeKey = foundation.NewKey[*audit.Scope](string(AuditProvider) + ".scope")

// AuditScope resolves a concrete handle during construction. Domain handlers
// retain this handle and call Within, rather than retaining constructor Services.
// It reuses the configured recorder, schema and exact database pool.
func (s Services) AuditScope() (*audit.Scope, error) { return Resolve(s, AuditScopeKey) }
