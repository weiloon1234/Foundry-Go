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

//foundry:model table=office_regions
type Region struct {
	ID model.ID[Region]
	// Code is the key shared by every polymorphic target type.
	Code       string
	Name       string
	Employees  relation.Many[Employee]
	Notes      relation.Many[Note]
	LatestHire relation.One[Employee]
}

//foundry:model table=office_offices
type Office struct {
	ID       model.ID[Office]
	Code     string
	RegionID model.ID[Region]
	Name     string
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
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

//foundry:model table=office_employees
type Employee struct {
	ID       model.ID[Employee]
	OfficeID model.ID[Office]
	Name     string
	Salary   int64
	HiredAt  time.Time
	Region   relation.One[Region]
}

func (Region) DefineRelations() RegionRelationSet {
	r, o, e, n := RegionFields(), OfficeFields(), EmployeeFields(), NoteFields()
	return RegionRelationSet{
		Employees:  query.HasManyThrough(r.ID, o.RegionID, o.ID, e.OfficeID),
		LatestHire: query.HasOneThrough(r.ID, o.RegionID, o.ID, e.OfficeID).OfMany(e.HiredAt.Desc()),
		Notes:      query.MorphMany(r.Code, n.SubjectID, n.SubjectType),
	}
}

// MorphName declares the stored discriminator of regions in polymorphic relationships.
func (Region) MorphName() query.MorphName { return "region" }

// MorphName declares the stored discriminator of offices in polymorphic relationships.
func (Office) MorphName() query.MorphName { return "office" }

// Note belongs to either a region or an office.
//
//foundry:model table=office_notes
type Note struct {
	ID          model.ID[Note]
	SubjectType query.MorphName
	SubjectID   string
	Body        string
	Region      relation.One[Region]
	Office      relation.One[Office]
}

func (Note) DefineRelations() NoteRelationSet {
	n, r, o := NoteFields(), RegionFields(), OfficeFields()
	return NoteRelationSet{
		Region: query.MorphTo(n.SubjectID, n.SubjectType, r.Code),
		Office: query.MorphTo(n.SubjectID, n.SubjectType, o.Code),
	}
}

//foundry:model table=office_labels
type Label struct {
	ID      model.ID[Label]
	Name    string
	Offices relation.Through[Office, Labeling]
}

//foundry:model table=office_labelings
type Labeling struct {
	ID            model.ID[Labeling]
	LabelID       model.ID[Label]
	LabelableType query.MorphName
	LabelableID   string
}

func (Label) DefineRelations() LabelRelationSet {
	l, p, o := LabelFields(), LabelingFields(), OfficeFields()
	return LabelRelationSet{Offices: query.MorphedByMany(l.ID, p.LabelID, p.LabelableType, p.LabelableID, o.Code)}
}

func (Office) DefineRelations() OfficeRelationSet {
	o, e, r := OfficeFields(), EmployeeFields(), RegionFields()
	n, p, l := NoteFields(), LabelingFields(), LabelFields()
	return OfficeRelationSet{
		Newest:     query.HasOne(o.ID, e.OfficeID).LatestOfMany(),
		Earliest:   query.HasOne(o.ID, e.OfficeID).OfMany(e.HiredAt.Asc()),
		TopEarner:  query.HasOne(o.ID, e.OfficeID).OfMany(e.Salary.Desc()),
		Region:     query.BelongsTo(o.RegionID, r.ID),
		Colleagues: query.HasMany(o.ID, e.OfficeID),
		Notes:      query.MorphMany(o.Code, n.SubjectID, n.SubjectType),
		Labels:     query.MorphToMany(o.Code, p.LabelableID, p.LabelableType, p.LabelID, l.ID),
		Desks:      query.HasMany(o.ID, DeskFields().OfficeID),
	}
}

func (Office) DefineAggregates() OfficeAggregateSet {
	return OfficeAggregateSet{Headcount: query.Related(OfficeRelations().Colleagues, query.Count[Employee]())}
}

func (Employee) DefineRelations() EmployeeRelationSet {
	e, o, r := EmployeeFields(), OfficeFields(), RegionFields()
	return EmployeeRelationSet{Region: query.HasOneThrough(e.OfficeID, o.ID, o.RegionID, r.ID)}
}

// Desk is visible only while its office is: its global scope is a relation
// existence predicate over the model's own relation.
//
//foundry:model table=office_desks
type Desk struct {
	ID       model.ID[Desk]
	OfficeID model.ID[Office]
	Label    string
	Office   relation.One[Office]
}

func (Desk) DefineRelations() DeskRelationSet {
	return DeskRelationSet{Office: query.BelongsTo(DeskFields().OfficeID, OfficeFields().ID)}
}

// OpenOffice hides desks whose office is soft-deleted. It is a function rather
// than a package variable: DeskRelations reads generated declarations, which a
// package variable's initializer would run before they exist.
func OpenOffice() query.GlobalScope[Desk] {
	return query.NewGlobalScope("open_office", DeskRelations().Office.Exists())
}

// DefineGlobalScopes runs lazily on the first query that needs the scopes, so
// it may use DeskRelations without re-entering the generated query declaration.
func (Desk) DefineGlobalScopes() []query.GlobalScope[Desk] {
	return []query.GlobalScope[Desk]{OpenOffice()}
}
