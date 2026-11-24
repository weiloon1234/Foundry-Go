package nestedbindings

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// FoundryIdentity preserves the stored team key used by authenticated bindings.
func (m Team) FoundryIdentity() (model.Identity, error) {
	return orm.IdentityOf(TeamRecords.Table, TeamRecords.Key()).ReferenceOf(m).Identity()
}

// TeamLinks owns the nested project scope. Example: TeamLinks.Projects.Where(orm.Eq(ProjectRecords.Enabled, true)).
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

func projectRetrievalHooks() orm.RetrievalHooks[Project] {
	return orm.RetrievalHooks[Project]{Retrieved: retrievedProject}
}
