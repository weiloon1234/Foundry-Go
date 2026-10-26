// Package officequeries exercises relationships through an intermediate model,
// one-of-many relationships, ad-hoc relation aggregates and a global scope
// declared with a relation-existence predicate.
package officequeries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Region struct {
	ID model.ID[Region]
	// Code is the key shared by every polymorphic target type.
	Code       string
	Name       string
	Employees  relation.Many[Employee]
	Notes      relation.Many[Note]
	LatestHire relation.One[Employee]
}

type Office struct {
	ID         model.ID[Office]
	Code       string
	RegionID   model.ID[Region]
	Name       string
	DeletedAt  value.Nullable[time.Time]
	Newest     relation.One[Employee]
	Earliest   relation.One[Employee]
	TopEarner  relation.One[Employee]
	Region     relation.One[Region]
	Headcount  relation.Value[int64]
	Colleagues relation.Many[Employee]
	Notes      relation.Many[Note]
	Labels     relation.Through[Label, Labeling]
	Desks      relation.Many[Desk]
}

type Employee struct {
	ID       model.ID[Employee]
	OfficeID model.ID[Office]
	Name     string
	Salary   int64
	HiredAt  time.Time
	Region   relation.One[Region]
}

// MorphName declares the stored discriminator of regions in polymorphic relationships.
func (Region) MorphName() query.MorphName { return "region" }

// MorphName declares the stored discriminator of offices in polymorphic relationships.
func (Office) MorphName() query.MorphName { return "office" }

// Note belongs to either a region or an office.
type Note struct {
	ID          model.ID[Note]
	SubjectType query.MorphName
	SubjectID   string
	Body        string
	Region      relation.One[Region]
	Office      relation.One[Office]
}

type Label struct {
	ID      model.ID[Label]
	Name    string
	Offices relation.Through[Office, Labeling]
}

type Labeling struct {
	ID            model.ID[Labeling]
	LabelID       model.ID[Label]
	LabelableType query.MorphName
	LabelableID   string
}

// Desk is visible only while its office is: its global scope is a relation
// existence predicate over the model's own relation.
type Desk struct {
	ID       model.ID[Desk]
	OfficeID model.ID[Office]
	Label    string
	Office   relation.One[Office]
}
