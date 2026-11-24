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

//foundry:model table=hook_members var=Members key=ID identity=false hooks=memberHooks
type Member struct {
	ID           model.ID[Member]
	Email        string                           `foundry:"input=string"`
	Nickname     value.Nullable[string]           `foundry:"input=string"`
	IntroducerID value.Nullable[model.ID[Member]] `foundry:"role=column"`
	IsIntroducer bool                             `foundry:"default=database,role=column"`
	Rank         int                              `foundry:"default=database"`
}

//foundry:model table=hook_logs var=Logs key=ID identity=false
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
