package countries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
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
		result, err = QueryFoundryCountries().Find(ctx, tx, code)
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
		result, err = QueryFoundryCountries().Where(CountryFields().ISO2.Eq(code)).Exists(ctx, tx)
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
		f := CountryFields()
		q := QueryFoundryCountries()
		if enabled {
			q = q.Where(f.Status.Eq(EnabledStatus))
		}
		rows, err := q.OrderBy(f.Name.Asc(), f.ISO2.Asc()).Limit(MaxCountries+1).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) > MaxCountries {
			return fault.New(fault.Conflict, "country list exceeds its limit")
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
		result, err = QueryFoundryCountries().Where(CountryFields().IsDefault.Eq(true)).First(ctx, tx)
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
		q, f := QueryFoundryCountries(), CountryFields()
		current, err := q.Where(f.IsDefault.Eq(true)).ForUpdate().All(ctx, tx)
		if err != nil {
			return err
		}
		for _, country := range current {
			if country.ISO2 == code {
				continue
			}
			if _, err := q.Update(ctx, tx, country.ISO2, CountryDraft{}.SetIsDefault(false)); err != nil {
				return err
			}
		}
		_, err = q.Update(ctx, tx, code, CountryDraft{}.SetIsDefault(true))
		return err
	})
}
