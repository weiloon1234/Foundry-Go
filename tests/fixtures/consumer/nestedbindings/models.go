// Package nestedbindings exercises alternate keys and nested route resources.
package nestedbindings

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

type Slug string

//foundry:model table=teams var=TeamRecords key=ID identity=false
type Team struct {
	ID       model.ID[Team]
	Name     string
	Projects relation.Many[Project]
}

//foundry:model table=projects var=ProjectRecords key=ID identity=false retrieval=projectRetrievalHooks
type Project struct {
	ID        model.ID[Project]
	TeamID    model.ID[Team] `foundry:"role=column"`
	Slug      Slug
	Enabled   bool                      `foundry:"role=column"`
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
	Team      relation.One[Team]
	Tasks     relation.Many[Task]
}

//foundry:model table=tasks var=TaskRecords key=ID identity=false
type Task struct {
	ID        model.ID[Task]
	ProjectID model.ID[Project] `foundry:"role=column"`
	Slug      Slug
}

type Trace struct {
	Projects int
	Fail     bool
}
type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

var RetrievalFailure = errors.New("test project retrieval failed")
