package officequeries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// doc-example:begin model-relations-3
var newest = orm.HasOneOf(Offices.Newest, Offices.ID, Employees.OfficeID).LatestOfMany()
var earliest = orm.HasOneOf(Offices.Earliest, Offices.ID, Employees.OfficeID).OfMany(orm.Asc(Employees.HiredAt))

// doc-example:end model-relations-3

// doc-example:begin model-relations-4
var employees = orm.HasManyThroughOf(Regions.Employees, orm.JoinKeys(Regions.ID, Offices.RegionID), orm.JoinKeys(Offices.ID, Employees.OfficeID))
var region = orm.HasOneThroughOf(Employees.Region, orm.JoinKeys(Employees.OfficeID, Offices.ID), orm.JoinKeys(Offices.RegionID, Regions.ID))

// doc-example:end model-relations-4
