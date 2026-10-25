// Package hookqueries exercises handwritten domain hooks through the ORM.
package hookqueries

import (
	"context"
	"errors"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Member struct {
	ID           model.ID[Member]
	Email        string
	Nickname     value.Nullable[string]
	IntroducerID value.Nullable[model.ID[Member]]
	IsIntroducer bool `foundry:"default=database"`
	Rank         int  `foundry:"default=database"`
}

type Log struct {
	ID    model.ID[Log]
	Label string
}

var Veto = errors.New("injected hook veto")

func (Member) MutateEmail(v string) (string, error) {
	return strings.ToLower(strings.TrimSpace(v)), nil
}
func (Member) MutateNickname(v string) (string, error) { return v + "!", nil }

// Trace is fixture instrumentation supplied per operation through context,
// keeping tests independent without a mutable package-wide hook registry.
type Trace struct {
	Inspect        func(*orm.Write[Member]) error
	Calls          []string
	ORMChanges     []orm.Changes[Member]
	ORMAfterCommit func(context.Context, lifecycle.Operation, orm.Changes[Member]) error
	Operations     []lifecycle.Operation
	FailAt         string
	Failure        string
	Cancel         context.CancelFunc
	Recurse        bool
}
type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}
