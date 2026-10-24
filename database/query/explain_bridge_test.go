package query

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"testing"
)

func TestPlanBridgeRejectsZeroCapabilityBeforeCompile(t *testing.T) {
	called := false
	_, err := FoundryExplain(t.Context(), nil, func(context.Context) (querycore.Statement, error) { called = true; return querycore.Statement{}, nil }, false, sqlowner.QuerySeal{})
	if !errors.Is(err, fault.Invalid) || called {
		t.Fatal(err, called)
	}
}
