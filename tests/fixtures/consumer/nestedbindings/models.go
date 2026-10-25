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

type Team struct {
	ID       model.ID[Team]
	Name     string
	Projects relation.Many[Project]
}

type Project struct {
	ID        model.ID[Project]
	TeamID    model.ID[Team]
	Slug      Slug
	Enabled   bool
	DeletedAt value.Nullable[time.Time]
	Team      relation.One[Team]
	Tasks     relation.Many[Task]
}

type Task struct {
	ID        model.ID[Task]
	ProjectID model.ID[Project]
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
