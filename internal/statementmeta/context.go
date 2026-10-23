// Package statementmeta carries compiler-owned diagnostics across execution.
package statementmeta

import "context"

type activeKey struct{}
type shapeKey struct{}
type compiled struct{ sql, shape string }

func Enable(ctx context.Context) context.Context { return context.WithValue(ctx, activeKey{}, true) }
func Active(ctx context.Context) bool            { active, _ := ctx.Value(activeKey{}).(bool); return active }

// With binds a compiler-owned shape to the exact statement, preventing a borrowed
// executor context from attributing different raw SQL to a trusted shape.
func With(ctx context.Context, sql, shape string) context.Context {
	if !Active(ctx) {
		return ctx
	}
	return context.WithValue(ctx, shapeKey{}, compiled{sql, shape})
}
func Shape(ctx context.Context, sql string) string {
	c, _ := ctx.Value(shapeKey{}).(compiled)
	if c.sql != sql {
		return ""
	}
	return c.shape
}
