package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// ProjectTable binds Project storage and typed relation slots.

// PersonTable binds Person storage and typed relation slots.

// AssignmentTable binds Assignment storage and typed relation slots.

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable {
	return []orm.CheckedTable{Projects.Table, People.Table, Assignments.Table}
}

var ProjectLinks = struct {
	Members orm.ThroughRelation[Project, Person, Assignment]
}{orm.ManyToManyOf(Projects.Members, orm.JoinKeys(Projects.ID, Assignments.ProjectID), orm.JoinKeys(Assignments.PersonID, People.ID))}

func foundryAssignmentHooks() orm.Hooks[Assignment] { return ORMHooks("local") }
