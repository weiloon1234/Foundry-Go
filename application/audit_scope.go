package application

import (
	"github.com/weiloon1234/Foundry-Go/audit"
	auditcommand "github.com/weiloon1234/Foundry-Go/audit/command"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var AuditScopeKey = foundation.NewKey[*audit.Scope](string(AuditProvider) + ".scope")

// AuditScope resolves a concrete handle during construction. Domain handlers
// retain this handle and call Within, rather than retaining constructor Services.
// It reuses the configured recorder, schema and exact database pool.
func (s Services) AuditScope() (*audit.Scope, error) { return Resolve(s, AuditScopeKey) }

// AuditCommand declares the explicit `audit prune` operator command. It resolves
// the configured audit scope after boot and removes history in bounded
// transactions using the configured retention or an explicit --before cutoff.
// Register it in the application's CLI registry; nothing prunes automatically.
func AuditCommand() (cli.Declaration, error) {
	return auditcommand.Declaration(func(r foundation.Resolver) (*audit.Scope, error) {
		return foundation.Resolve(r, AuditScopeKey)
	}, clock.System{})
}
