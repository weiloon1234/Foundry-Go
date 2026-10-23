// Package lifecore shares callback nesting across generated and declared models.
package lifecore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxDepth bounds nested write and retrieval callbacks across both APIs.
const MaxDepth = 32

type depthKey struct{}

// Context retains request metadata and increments the shared callback depth.
func Context(ctx context.Context) (context.Context, error) {
	depth, _ := ctx.Value(depthKey{}).(int)
	if depth >= MaxDepth {
		return nil, fault.New(fault.Invalid, "model lifecycle nesting exceeds its depth bound; check recursive hooks")
	}
	return context.WithValue(ctx, depthKey{}, depth+1), nil
}
