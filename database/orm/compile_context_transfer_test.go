package orm

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"testing"
)

type diagnosticTransferExecutor struct {
	database.Executor
	shape string
	cause error
}

func (e *diagnosticTransferExecutor) Query(ctx context.Context, sql string, _ ...any) (*database.Rows, error) {
	e.shape = statementmeta.Shape(ctx, sql)
	return nil, e.cause
}
func TestCompileInOverridesStoredDiagnosticScope(t *testing.T) {
	active := statementmeta.Enable(t.Context())
	d := scopeTransferFields()
	sentinel := errors.New("captured")
	for _, stored := range []context.Context{context.Background(), active} {
		q := WithScopeContext(d.Query(), stored)
		for _, ctx := range []context.Context{context.Background(), active, context.Background()} {
			ex := &diagnosticTransferExecutor{cause: sentinel}
			_, e := All(ctx, ex, q)
			if !errors.Is(e, sentinel) {
				t.Fatal(e)
			}
			if (ex.shape != "") != statementmeta.Active(ctx) {
				t.Fatal("stored diagnostic scope won")
			}
		}
	}
}
