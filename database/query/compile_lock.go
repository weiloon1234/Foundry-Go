package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func compileLock(node selectNode, spec lockSpec) (string, error) {
	return querycore.CompileLock(node, spec)
}

func lockReference(table string) string { return querycore.LockReference(table) }

func validateLockSources(node selectNode, targets []string, explicit bool, depth int) (int, error) {
	return querycore.ValidateLockSources(node, targets, explicit, depth)
}

func validateLockValue(v valueExpression) error { return querycore.ValidateLockValue(v) }
