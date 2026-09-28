package generate

import (
	"go/types"
	"slices"
)

func discoverSoftDeletes(p *packageInput, m *model, setting string) error {
	if setting != "" && setting != "true" && setting != "false" {
		return p.diagnostic(m.typ.Obj().Pos(), "soft_deletes must be true or false")
	}
	if setting == "false" {
		return nil
	}
	index := slices.IndexFunc(m.fields, func(f field) bool { return f.name == "DeletedAt" })
	if index < 0 {
		if setting == "true" {
			return p.diagnostic(m.typ.Obj().Pos(), "soft_deletes=true requires a persisted DeletedAt field")
		}
		return nil
	}
	f := &m.fields[index]
	named, ok := types.Unalias(f.base).(*types.Named)
	if !ok || !f.nullable || f.primary || (!isNamed(named, "time", "Time") && !isNamed(named, framework+"/temporal", "DateTime")) {
		return p.diagnostic(m.typ.Obj().Pos(), "soft deletion requires nullable time.Time or temporal.DateTime in a non-primary DeletedAt field")
	}
	if f.input != nil {
		return p.diagnostic(m.typ.Obj().Pos(), "soft deletion cannot synthesize distinct mutator inputs")
	}
	f.softDelete = true
	m.softDelete = f.column
	return nil
}
