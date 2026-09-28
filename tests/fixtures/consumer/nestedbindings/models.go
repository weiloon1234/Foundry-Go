// Package nestedbindings exercises alternate keys and nested route resources.
package nestedbindings

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

type Slug string

//foundry:model table=teams
type Team struct {
	ID       model.ID[Team]
	Name     string
	Projects relation.Many[Project]
}

//foundry:model table=projects retrieval=projectRetrieval
type Project struct {
	ID      model.ID[Project]
	TeamID  model.ID[Team]
	Slug    Slug
	Enabled bool
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[time.Time]
	Team      relation.One[Team]
	Tasks     relation.Many[Task]
}

//foundry:model table=tasks
type Task struct {
	ID        model.ID[Task]
	ProjectID model.ID[Project]
	Slug      Slug
}

func (Team) DefineRelations() TeamRelationSet {
	return TeamRelationSet{Projects: query.HasMany(TeamFields().ID, ProjectFields().TeamID)}
}
func (Project) DefineRelations() ProjectRelationSet {
	return ProjectRelationSet{Team: query.BelongsTo(ProjectFields().TeamID, TeamFields().ID), Tasks: query.HasMany(ProjectFields().ID, TaskFields().ProjectID)}
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

func projectRetrieval() ProjectRetrievalHooks {
	return ProjectRetrievalHooks{Retrieved: func(ctx context.Context, _ database.Executor, _ Project) error {
		if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil {
			trace.Projects++
			if trace.Fail {
				return RetrievalFailure
			}
		}
		return nil
	}}
}
