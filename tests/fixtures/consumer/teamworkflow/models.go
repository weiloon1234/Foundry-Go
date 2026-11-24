// Package teamworkflow is a complete, independently compiled typed API workflow.
package teamworkflow

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Slug string

//foundry:model table=workflow_teams var=Teams key=ID identity=false
type Team struct {
	// Table owns the primary key. Example: orm.Eq(Teams.Key(), id).
	ID int64
	// Tenant binds the Tenant field. Example: orm.Set(Teams.Tenant, "tenant").
	Tenant string
	// Projects binds the Projects field. Example: Teams.Query().With(TeamProjects).
	Projects relation.Many[Project]
}

//foundry:model table=workflow_projects var=Projects key=ID identity=false
type Project struct {
	// Table owns the primary key. Example: orm.Eq(Projects.Key(), id).
	ID model.ID[Project]
	// TeamID binds the TeamID field. Example: orm.Set(Projects.TeamID, 1).
	TeamID int64
	// Slug binds the Slug field. Example: orm.Set(Projects.Slug, "shared").
	Slug Slug
	// Title binds the Title field. Example: orm.Set(Projects.Title, "Title").
	Title value.Nullable[string]
	// Budget binds the Budget field. Example: orm.Set(Projects.Budget, 0).
	Budget int64
	// Enabled binds the Enabled field. Example: orm.Set(Projects.Enabled, true).
	Enabled bool `foundry:"role=column"`
}

//foundry:model table=workflow_submissions var=Submissions key=ID identity=false
type Submission struct {
	// Table owns the primary key. Example: orm.Eq(Submissions.Key(), id).
	ID model.ID[Submission]
	// ProjectID binds the ProjectID field. Example: orm.Set(Submissions.ProjectID, id).
	ProjectID model.ID[Project] `foundry:"role=column"`
	// Name binds the Name field. Example: orm.Set(Submissions.Name, "entry").
	Name string
}
