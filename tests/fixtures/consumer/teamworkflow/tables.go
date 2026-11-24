package teamworkflow

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// TeamProjects scopes child lookup to its owning team. Example: Teams.Query().With(TeamProjects).
var TeamProjects = orm.HasManyOf(Teams.Projects, Teams.Key(), Projects.TeamID)

// Tables lists declarations for startup validation. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Teams, Projects, Submissions} }
