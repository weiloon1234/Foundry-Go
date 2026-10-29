// Package observerqueries verifies provider observers through ORM writes.
package observerqueries

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Record struct {
	ID   model.ID[Record]
	Name string
}

type Plain struct {
	ID   model.ID[Plain]
	Name string
}

type Effect struct {
	ID    model.ID[Effect]
	Label string
}

func (Record) MutateName(name string) (string, error) {
	return strings.ToLower(strings.TrimSpace(name)) + "!", nil
}

var Veto = errors.New("observer veto")

type Trace struct {
	Inspect    func(*orm.Write[Record]) error
	Calls      []string
	ORMChanges []orm.Changes[Record]
	FailAt     string
	Mode       string
	Cancel     context.CancelFunc
}
type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}
func traceOf(ctx context.Context) *Trace { trace, _ := ctx.Value(traceKey{}).(*Trace); return trace }

// Stats belongs to the application fixture; factories may run concurrently.
type Stats struct {
	First     atomic.Int64
	Second    atomic.Int64
	Plain     atomic.Int64
	Retrieval atomic.Int64
}

func record(ctx context.Context, tx *database.Tx, trace *Trace, label string) error {
	if trace != nil {
		trace.Calls = append(trace.Calls, label)
	}
	if tx != nil {
		if _, err := orm.Insert(ctx, tx, Effects.Table, orm.TextSet(Effects.Label, label)); err != nil {
			return err
		}
	}
	if trace == nil || trace.FailAt != label {
		return nil
	}
	switch trace.Mode {
	case "panic":
		panic("observer panic")
	case "goexit":
		runtime.Goexit()
	case "cancel":
		trace.Cancel()
		return nil
	}
	return Veto
}
