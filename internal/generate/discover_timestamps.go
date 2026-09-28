package generate

import (
	"go/types"
	"slices"
)

func discoverTimestamps(p *packageInput, m *model, setting string) error {
	if setting != "" && setting != "true" && setting != "false" {
		return p.diagnostic(m.typ.Obj().Pos(), "timestamps must be true or false")
	}
	if setting == "false" {
		return nil
	}
	created := slices.IndexFunc(m.fields, func(f field) bool { return f.name == "CreatedAt" })
	updated := slices.IndexFunc(m.fields, func(f field) bool { return f.name == "UpdatedAt" })
	if created < 0 || updated < 0 {
		if setting == "true" {
			return p.diagnostic(m.typ.Obj().Pos(), "timestamps=true requires persisted CreatedAt and UpdatedAt fields")
		}
		return nil
	}
	for role, index := range []int{created, updated} {
		f := &m.fields[index]
		named, ok := types.Unalias(f.base).(*types.Named)
		if !ok || f.nullable || f.primary || (!isNamed(named, "time", "Time") && !isNamed(named, framework+"/temporal", "DateTime")) {
			return p.diagnostic(m.typ.Obj().Pos(), "managed timestamps require non-null time.Time or temporal.DateTime fields that are not primary keys")
		}
		if f.input != nil {
			return p.diagnostic(m.typ.Obj().Pos(), "managed timestamps cannot synthesize distinct mutator inputs")
		}
		m.timestamps[role] = f.column
		if role == 0 {
			f.timestamp = "created"
		} else {
			f.timestamp = "updated"
		}
	}
	return nil
}
