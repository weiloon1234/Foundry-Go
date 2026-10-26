// Package pivothooks exercises many-to-many synchronization through a pivot
// model with local lifecycle hooks and a registered provider observer.
package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Project struct {
	ID      model.ID[Project]
	Name    string
	Members relation.Through[Person, Assignment]
}

type Person struct {
	ID   model.ID[Person]
	Name string
}

// Assignment is the pivot; every normal write runs its hooks and observers.
type Assignment struct {
	ID        model.ID[Assignment]
	ProjectID model.ID[Project]
	PersonID  model.ID[Person]
	Role      string
}
