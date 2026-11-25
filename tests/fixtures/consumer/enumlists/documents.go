// Package enumlists declares enum columns whose database constraints list their cases.
package enumlists

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin enum-list-declarations

//foundry:enum
type Status string

const (
	Draft     Status = "draft"
	Published Status = "published"
	Archived  Status = "archived"
)

//foundry:enum
type Role string

const (
	Reader Role = "reader"
	Editor Role = "editor"
)

//foundry:model table=enum_list_documents var=Documents key=ID
type Document struct {
	ID    model.ID[Document]
	Title string
	// Status holds every Status case; its constraint lists them all.
	Status Status
	// Roles holds a JSON array of Role cases; its constraint lists them all.
	Roles value.JSON[[]Role] `foundry:"default=database"`
	// Stage is a deliberately narrower column: it never holds Archived.
	Stage Status `foundry:"values=Draft|Published"`
}

// doc-example:end enum-list-declarations
