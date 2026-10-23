package statementmeta

import (
	"context"
	"testing"
)

func TestExactStatementBindingAndDisabledPath(t *testing.T) {
	ctx := context.Background()
	if With(ctx, "sql", "shape") != ctx {
		t.Fatal("disabled metadata allocated")
	}
	ctx = With(Enable(ctx), "compiled", "trusted")
	if Shape(ctx, "compiled") != "trusted" || Shape(ctx, "raw canary") != "" {
		t.Fatal("metadata not bound to exact SQL")
	}
}
