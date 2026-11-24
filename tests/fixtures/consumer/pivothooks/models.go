// Package pivothooks exercises many-to-many synchronization through a pivot
// model with local lifecycle hooks and a registered provider observer.
package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:model table=hook_projects var=Projects key=ID identity=false
type Project struct {
	ID      model.ID[Project] `foundry:"member=ID"`
	Name    string
	Members relation.Through[Person, Assignment]
}

//foundry:model table=hook_people var=People key=ID identity=false
type Person struct {
	ID   model.ID[Person] `foundry:"member=ID"`
	Name string
}

// Assignment is the pivot; every normal write runs its hooks and observers.
//
//foundry:model table=hook_assignments var=Assignments key=ID hooks=foundryAssignmentHooks identity=false
type Assignment struct {
	ID        model.ID[Assignment] `foundry:"member=ID"`
	ProjectID model.ID[Project]
	PersonID  model.ID[Person]
	Role      string
}
