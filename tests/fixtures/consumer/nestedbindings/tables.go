package nestedbindings

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// TeamTable binds the parent and its declared children. Example: TeamRecords.Query().
type TeamTable struct {
	orm.Table[Team, model.ID[Team]]
	Name     orm.Text[Team, string]
	Projects orm.Many[Team, Project]
}

// ProjectTable retains soft visibility and the concrete parent. Example: ProjectRecords.Slug.Eq(slug).
type ProjectTable struct {
	orm.Table[Project, model.ID[Project]]
	TeamID    orm.Column[Project, model.ID[Team]]
	Slug      orm.Text[Project, Slug]
	Enabled   orm.Column[Project, bool]
	DeletedAt orm.SoftDelete[Project, time.Time]
	Soft      orm.SoftDeletable[Project]
	Team      orm.One[Project, Team]
	Tasks     orm.Many[Project, Task]
}

// TaskTable binds each task to its owning project. Example: TaskRecords.Query().
type TaskTable struct {
	orm.Table[Task, model.ID[Task]]
	ProjectID orm.Column[Task, model.ID[Project]]
	Slug      orm.Text[Task, Slug]
}

// TeamRecords preserves the teams table. Example: TeamRecords.Key().Eq(id).
var TeamRecords = orm.Define("teams", func(b orm.Builder[Team], m *Team) TeamTable {
	return TeamTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name), orm.ManyOf(b, &m.Projects)}
})

// FoundryIdentity preserves the stored team key used by authenticated bindings.
func (m Team) FoundryIdentity() (model.Identity, error) {
	return orm.IdentityOf(TeamRecords.Table, TeamRecords.Key()).ReferenceOf(m).Identity()
}

// ProjectRecords dispatches complete-model retrieval after hydration. Example: ProjectRecords.Query().
var ProjectRecords = orm.Define("projects", func(b orm.Builder[Project], m *Project) ProjectTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return ProjectTable{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.TeamID), orm.TextOf(b, &m.Slug), orm.ColumnOf(b, &m.Enabled), deleted, orm.SoftDeletes(deleted), orm.OneOf(b, &m.Team), orm.ManyOf(b, &m.Tasks)}
}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[Project] { return orm.RetrievalHooks[Project]{Retrieved: retrievedProject} }))

// TaskRecords preserves the tasks table. Example: TaskRecords.Slug.Set(slug).
var TaskRecords = orm.Define("tasks", func(b orm.Builder[Task], m *Task) TaskTable {
	return TaskTable{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.ProjectID), orm.TextOf(b, &m.Slug)}
})

// TeamLinks owns the nested project scope. Example: TeamLinks.Projects.Where(ProjectRecords.Enabled.Eq(true)).
var TeamLinks = struct{ Projects orm.HasMany[Team, Project] }{orm.HasManyOf(TeamRecords.Projects, TeamRecords.Key(), ProjectRecords.TeamID)}

// ProjectLinks owns the parent and task scopes. Example: ProjectLinks.Tasks.
var ProjectLinks = struct {
	Team  orm.BelongsTo[Project, Team]
	Tasks orm.HasMany[Project, Task]
}{
	orm.BelongsToOf(ProjectRecords.Team, ProjectRecords.TeamID, TeamRecords.Key()), orm.HasManyOf(ProjectRecords.Tasks, ProjectRecords.Key(), TaskRecords.ProjectID),
}

func retrievedProject(ctx context.Context, _ database.Executor, _ Project) error {
	if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil {
		trace.Projects++
		if trace.Fail {
			return RetrievalFailure
		}
	}
	return nil
}
