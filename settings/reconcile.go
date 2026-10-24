package settings

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
)

// Reconciliation reports what Reconcile changed, or which stored settings it
// could not accept. Names are registered setting names only, never values.
type Reconciliation struct {
	// Upgraded values were rewritten from an earlier version by a declared upgrade.
	Upgraded []Name
	// Presented presentations still owned by their declaration were refreshed.
	Presented []Name
	// Incompatible rows have a newer version, or an older version without a
	// declared upgrade. Their presence fails Reconcile without any write.
	Incompatible []Name
}

// Reconcile brings stored settings in line with their registrations in one
// transaction: declared upgrades are applied and declaration-owned presentations
// are refreshed. Run it after migrations whenever a deployment changes setting
// versions or presentation, for example while starting serve and worker
// processes, and treat an error as a startup failure. An incompatible version
// fails with fault.Conflict, lists the settings in Incompatible and writes
// nothing, instead of letting every later Get fail. It reads presentation
// columns first and loads one value at a time for upgrades.
func Reconcile(ctx context.Context, m *Manager) (Reconciliation, error) {
	if err := m.Validate(); err != nil {
		return Reconciliation{}, err
	}
	names := make([]string, 0, len(m.keys))
	all := make([]Name, 0, len(m.keys))
	for name := range m.keys {
		names = append(names, string(name))
		all = append(all, name)
	}
	slices.Sort(names)
	var result Reconciliation
	defer m.invalidate(all...)
	m.invalidate(all...)
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		result = Reconciliation{}
		if len(names) == 0 {
			return nil
		}
		f := store.Settings
		q := store.Settings.Query()
		states, err := rows.ForUpdate(rows.Read(store.SettingStates(q.Where(f.Name.In(names...)).OrderBy(f.Name.Asc()).Limit(MaxKeys+1)))).All(ctx, tx)
		if err != nil {
			return err
		}
		var upgrades, presented []Name
		for _, state := range states {
			registration, ok := m.keys[Name(state.Name)]
			if !ok {
				return invalid()
			}
			if state.Version != uint32(registration.version) {
				if _, ok := registration.upgrades[Version(state.Version)]; !ok {
					result.Incompatible = append(result.Incompatible, registration.name)
					continue
				}
				upgrades = append(upgrades, registration.name)
			}
			if state.PresentationDeclared {
				same, err := samePresentation(state, registration.presentation)
				if err != nil {
					return err
				}
				if !same {
					presented = append(presented, registration.name)
				}
			}
		}
		if len(result.Incompatible) > 0 {
			return errNoUpgrade()
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		for _, name := range upgrades {
			row, err := q.Where(store.Settings.Key().Eq(string(name))).Require(ctx, tx)
			if err != nil {
				return err
			}
			converted, err := m.keys[name].upgrades[Version(row.Version)](ctx, row.Value)
			if err != nil {
				return err
			}
			if _, err := orm.Update(ctx, tx, q, store.Settings.Key(), string(name), store.Settings.Value.Set(converted), store.Settings.Version.Set(uint32(m.keys[name].version)), store.Settings.UpdatedAt.Set(now)); err != nil {
				return err
			}
		}
		for _, name := range presented {
			if _, err := orm.Update(ctx, tx, q, store.Settings.Key(), string(name), append(presentationDraft([]orm.Assignment[store.Setting]{}, m.keys[name].presentation), store.Settings.UpdatedAt.Set(now))...); err != nil {
				return err
			}
		}
		result.Upgraded, result.Presented = upgrades, presented
		return nil
	})
	if err != nil {
		return Reconciliation{Incompatible: result.Incompatible}, err
	}
	return result, nil
}
func samePresentation(state store.SettingState, declared Presentation) (bool, error) {
	stored, err := state.Parameters.Text()
	if err != nil {
		return false, err
	}
	parameters, err := declared.Parameters.Text()
	if err != nil {
		return false, err
	}
	return state.Kind == string(declared.Kind) && state.GroupName == string(declared.Group) && state.Label == declared.Label && state.Description == declared.Description && state.SortOrder == declared.Order && state.IsPublic == declared.Public && stored == parameters, nil
}
