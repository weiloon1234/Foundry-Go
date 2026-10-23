package positive

import (
	"context"

	"foundry.test/goway/models"
	db "github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

// Same spelling without the complete raw signature does not flag a value.
func UnrelatedValues(ctx context.Context,
	result func(context.Context, string, ...any) (int, error),
	nonvariadic func(context.Context, string, []any) (*db.Rows, error),
	inconsistentCodec func(codec.Codec[string], string, ...any) expr.Selected[models.User, int64],
) {
	_, _ = result(ctx, "value")
	_, _ = (*(&nonvariadic))(ctx, "value", nil)
	_ = inconsistentCodec(codec.String[string](), "value")
	f := func() int { return 1 }
	_ = f()
	f = func() int { return 2 }
	_ = f()
}

func InvokeUnrelated[F ~func(context.Context, string, ...any) (int, error)](ctx context.Context, f F) {
	_, _ = f(ctx, "ordinary")
}
func InvokeUnresolved[R any, F ~func(context.Context, string, ...any) (R, error)](ctx context.Context, f F) {
	// This constraint does not fix a framework raw result signature.
	_, _ = f(ctx, "ordinary")
}
