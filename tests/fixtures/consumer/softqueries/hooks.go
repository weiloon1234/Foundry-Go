package softqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var Veto = errors.New("soft-delete fixture veto")

type Trace struct {
	Steps      []string
	ORMChanges []orm.Changes[Member]
	Operations []lifecycle.Operation
	Committed  []lifecycle.Operation
	VetoAt     string
	CancelAt   string
	Cancel     context.CancelFunc
}

type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}
