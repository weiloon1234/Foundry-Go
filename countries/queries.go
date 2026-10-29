package countries

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxCountries = 1000

func Find(ctx context.Context, store *extensions.Store, code Code) (value.Optional[Country], error) {
	if err := code.Validate(); err != nil {
		return value.Optional[Country]{}, err
	}
	var result value.Optional[Country]
	err := store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = orm.Find(ctx, tx, Countries.Table, code)
		return err
	})
	if err != nil {
		return value.Optional[Country]{}, err
	}
	return result, nil
}
func Exists(ctx context.Context, store *extensions.Store, code Code) (bool, error) {
	if err := code.Validate(); err != nil {
		return false, err
	}
	result := false
	err := store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = orm.Exists(ctx, tx, Countries.Query().Where(orm.KeyEq(Countries.ISO2, code)))
		return err
	})
	if err != nil {
		return false, err
	}
	return result, nil
}
func All(ctx context.Context, store *extensions.Store) ([]Country, error) {
	return list(ctx, store, false)
}
func Enabled(ctx context.Context, store *extensions.Store) ([]Country, error) {
	return list(ctx, store, true)
}
func list(ctx context.Context, store *extensions.Store, enabled bool) ([]Country, error) {
	var result []Country
	err := store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		f := Countries
		q := Countries.Query()
		if enabled {
			q = q.Where(orm.EnumEq(f.Status, EnabledStatus))
		}
		rows, err := orm.All(ctx, tx, q.OrderBy(orm.TextAsc(f.Name), orm.KeyAsc(f.ISO2)).Limit(MaxCountries+1))
		if err != nil {
			return err
		}
		if len(rows) > MaxCountries {
			return safeerror.NewFault(fault.Conflict, "country list exceeds its limit")
		}
		result = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Default returns the application's default country, if one is marked. The
// single-default migration guarantees at most one row.
func Default(ctx context.Context, store *extensions.Store) (value.Optional[Country], error) {
	var result value.Optional[Country]
	err := store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = orm.First(ctx, tx, Countries.Query().Where(orm.ColumnEq(Countries.IsDefault, true)))
		return err
	})
	if err != nil {
		return value.Optional[Country]{}, err
	}
	return result, nil
}

// SetDefault atomically moves the default flag to an existing country. The
// previous default is cleared first in the same transaction, so the unique
// single-default index never observes two defaults. A missing code returns
// database.NotFound and leaves the current default unchanged.
func SetDefault(ctx context.Context, store *extensions.Store, code Code) error {
	if err := code.Validate(); err != nil {
		return err
	}
	return store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		q, f := Countries.Query(), Countries
		current, err := orm.AllLocked(ctx, tx, orm.Lock(q.Where(orm.ColumnEq(f.IsDefault, true))))
		if err != nil {
			return err
		}
		for _, country := range current {
			if country.ISO2 == code {
				continue
			}
			if _, err := orm.Update(ctx, tx, Countries.Table.Query(), Countries.Table.Key(), country.ISO2, orm.ColumnSet(Countries.IsDefault, false)); err != nil {
				return err
			}
		}
		_, err = orm.Update(ctx, tx, Countries.Table.Query(), Countries.Table.Key(), code, orm.ColumnSet(Countries.IsDefault, true))
		return err
	})
}
