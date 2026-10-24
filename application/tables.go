package application

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// WithTables validates the application's explicit table declarations before any
// providers are constructed. Build reports all declaration errors from orm.Check.
// The list is copied; declarations themselves are immutable after orm.Define.
// Example: application.New(settings, application.WithTables(models.Orders.Table)).
func WithTables(tables ...orm.CheckedTable) Option {
	tables = slices.Clone(tables)
	return func(*options) error {
		return orm.Check(tables...)
	}
}
