// Package pivothooks exercises many-to-many synchronization through a pivot
// model with local lifecycle hooks and a registered provider observer.
package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:model table=hook_projects
type Project struct {
	ID      model.ID[Project]
	Name    string
	Members relation.Through[Person, Assignment]
}

//foundry:model table=hook_people
type Person struct {
	ID   model.ID[Person]
	Name string
}

// Assignment is the pivot; every normal write runs its hooks and observers.
//
//foundry:model table=hook_assignments hooks=assignmentHooks
type Assignment struct {
	ID        model.ID[Assignment]
	ProjectID model.ID[Project]
	PersonID  model.ID[Person]
	Role      string
}

func (Project) DefineRelations() ProjectRelationSet {
	p, a, m := ProjectFields(), AssignmentFields(), PersonFields()
	return ProjectRelationSet{Members: query.ManyToMany(p.ID, a.ProjectID, a.PersonID, m.ID)}
}
