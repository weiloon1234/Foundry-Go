package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// ProjectTable binds Project storage and typed relation slots.
type ProjectTable struct {
	orm.Table[Project, model.ID[Project]]
	ID      orm.Key[Project, model.ID[Project]]
	Name    orm.Text[Project, string]
	Members orm.Through[Project, Person, Assignment]
}

var Projects = orm.Define("hook_projects", func(b orm.Builder[Project], m *Project) ProjectTable {
	table := orm.TableOf(b, &m.ID)
	return ProjectTable{Table: table, ID: table.Key(), Name: orm.TextOf(b, &m.Name), Members: orm.ThroughOf(b, &m.Members)}
})

// PersonTable binds Person storage and typed relation slots.
type PersonTable struct {
	orm.Table[Person, model.ID[Person]]
	ID   orm.Key[Person, model.ID[Person]]
	Name orm.Text[Person, string]
}

var People = orm.Define("hook_people", func(b orm.Builder[Person], m *Person) PersonTable {
	table := orm.TableOf(b, &m.ID)
	return PersonTable{Table: table, ID: table.Key(), Name: orm.TextOf(b, &m.Name)}
})

// AssignmentTable binds Assignment storage and typed relation slots.
type AssignmentTable struct {
	orm.Table[Assignment, model.ID[Assignment]]
	ID        orm.Key[Assignment, model.ID[Assignment]]
	ProjectID orm.Column[Assignment, model.ID[Project]]
	PersonID  orm.Column[Assignment, model.ID[Person]]
	Role      orm.Text[Assignment, string]
}

var Assignments = orm.Define("hook_assignments", func(b orm.Builder[Assignment], m *Assignment) AssignmentTable {
	table := orm.TableOf(b, &m.ID)
	return AssignmentTable{Table: table, ID: table.Key(), ProjectID: orm.IDOf(b, &m.ProjectID), PersonID: orm.IDOf(b, &m.PersonID), Role: orm.TextOf(b, &m.Role)}
}, orm.WithHooks(func() orm.Hooks[Assignment] { return ORMHooks("local") }))

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable {
	return []orm.CheckedTable{Projects.Table, People.Table, Assignments.Table}
}

var ProjectLinks = struct {
	Members orm.ThroughRelation[Project, Person, Assignment]
}{orm.ManyToManyOf(Projects.Members, orm.KeyExpr(Projects.ID), orm.ColumnExpr(Assignments.ProjectID), orm.ColumnExpr(Assignments.PersonID), orm.KeyExpr(People.ID))}
