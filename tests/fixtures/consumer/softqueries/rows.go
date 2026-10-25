package softqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberLabelRow declares a complete DTO projection. Example: MemberLabels.Name.
type MemberLabelRow struct {
	rows.Row[MemberLabel]
	ID   rows.Field[MemberLabel, model.ID[Member]]
	Name rows.Field[MemberLabel, string]
}

// MemberLabels preserves the projection's stored fields. Example: rows.Select(source,MemberLabels,...).
var MemberLabels = rows.Define(func(b rows.Builder[MemberLabel], m *MemberLabel) MemberLabelRow {
	return MemberLabelRow{b.Row(), rows.IDOf(b, &m.ID), rows.TextOf(b, &m.Name)}
})
