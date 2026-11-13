package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// CheckCollation verifies that the configured TextCollation resolves in the
// report schema. Module calls it during boot; direct managers can call it before
// serving requests. Empty configuration performs no database I/O. Config.Timeout
// and the caller's context bound the check. Per-expression collations are unchecked.
//
// Example: manager.CheckCollation(ctx)
func (m *Manager) CheckCollation(ctx context.Context) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.config.TextCollation == "" {
		return nil
	}
	return m.calls.Run(ctx, "datatable collation check", func(ctx context.Context) error {
		var exists bool
		if err := m.read(ctx, func(tx *database.Tx) error {
			return database.ScanOne(ctx, tx, "SELECT to_regcollation($1) IS NOT NULL", []any{querycore.Quoted(string(m.config.TextCollation))}, &exists)
		}); err != nil {
			return err
		}
		if !exists {
			return invalid("report text collation is not available in the database")
		}
		return nil
	})
}
