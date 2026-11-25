package generate

import "go/types"

// Match configureConventions in database/orm: explicit roles displace names;
// ordinary names activate timestamps only as a pair, but deletion on its own.
// TestModelTimestampConventions checks accepted output and diagnostic fixes
// through the ORM so these static checks cannot silently drift from runtime.
func validateModelTimestamps(p *packageInput, d *modelDeclaration, fields, roles map[string]modelField) error {
	fail := func(f modelField, message string) error {
		return modelError(p, f.field.Pos(), d.name, f.field.Name(), message)
	}
	ordinary := make(map[string]modelField)
	for name, f := range fields {
		switch f.carrier {
		case "CreatedAt", "UpdatedAt", "SoftDelete":
		default:
			ordinary[name] = f
		}
	}
	created, hasCreated := roles["CreatedAt"]
	updated, hasUpdated := roles["UpdatedAt"]
	if hasCreated != hasUpdated {
		f, missing := created, "UpdatedAt"
		if hasUpdated {
			f, missing = updated, "CreatedAt"
		}
		message := "use timestamps=none on the model and remove timestamp role tags"
		if partner, exists := ordinary[missing]; !exists || modelManagedInstant(partner, false) {
			message = "declare both role=created-at and role=updated-at, or " + message
		}
		return fail(f, "only one of the managed timestamp roles is declared; "+message)
	}
	// Preserve the existing explicit plain-column convention opt-out.
	if !hasCreated {
		for _, name := range []string{"CreatedAt", "UpdatedAt"} {
			if f, exists := ordinary[name]; exists && (f.tag.Role == "column" || f.tag.Codec != "") {
				d.noTimestamps = true
			}
		}
	}
	if !d.hasSoft {
		if f, exists := ordinary["DeletedAt"]; exists && (f.tag.Role == "column" || f.tag.Codec != "") {
			d.noSoftDeletes = true
		}
	}
	if !d.noTimestamps && !hasCreated {
		c, hasC := ordinary["CreatedAt"]
		u, hasU := ordinary["UpdatedAt"]
		if hasC && hasU {
			message := "use timestamps=none on the model and remove timestamp role tags"
			if modelManagedInstant(c, false) && modelManagedInstant(u, false) {
				message = "declare role=created-at on CreatedAt and role=updated-at on UpdatedAt, or " + message
			}
			return fail(c, "CreatedAt and UpdatedAt are conventional timestamp names; "+message)
		}
	}
	if !d.noSoftDeletes && !d.hasSoft {
		if f, exists := ordinary["DeletedAt"]; exists {
			message := "use softdeletes=none on the model and remove its role tag"
			if modelManagedInstant(f, true) {
				message = "declare role=soft-delete on DeletedAt, or " + message
			}
			return fail(f, "DeletedAt is a conventional deletion name; "+message)
		}
	}
	// Only after resolving all roles and names can an ordinary time spelling be
	// offered: a field-local suggestion cannot know whether its partner exists.
	for _, f := range d.fields {
		if f.key || f.slot || f.tag.Role != "" || f.tag.Codec != "" {
			continue
		}
		switch f.carrier {
		case "Time", "NullableTime", "InputTime", "NullableInputTime":
		default:
			continue
		}
		switch f.field.Name() {
		case "CreatedAt", "UpdatedAt":
			if d.noTimestamps {
				continue
			}
		case "DeletedAt":
			if d.noSoftDeletes {
				continue
			}
		default:
			continue
		}
		return fail(f, "a conventional timestamp name on an ordinary time column; declare role=time for an ordinary time column")
	}
	return nil
}

func modelManagedInstant(f modelField, nullable bool) bool {
	n, ok := types.Unalias(f.stored).(*types.Named)
	return ok && (isNamed(n, "time", "Time") || isNamed(n, framework+"/temporal", "DateTime")) && f.nullable == nullable && !f.key && !f.tag.DatabaseDefault && f.input == nil
}
