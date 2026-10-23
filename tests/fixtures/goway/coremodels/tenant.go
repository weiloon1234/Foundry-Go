package coremodels

import (
	"context"
	"errors"
)

type tenantKey struct{}

func TenantContext(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, tenantKey{}, id)
}
func TenantID(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(tenantKey{}).(int64)
	if !ok {
		return 0, errors.New("tenant is missing")
	}
	return id, nil
}
