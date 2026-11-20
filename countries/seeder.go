package countries

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const SeederID seed.ID = "foundry.countries"

// SeedResult reports the reference data version and seeded row count.
type SeedResult struct {
	Version string
	Rows    int
}

// Seeder plugs into the ordinary explicit transactional seeder registry. It
// never installs itself, migrates tables, fetches data or runs during app boot.
func Seeder(store *extensions.Store) seed.Definition {
	return seed.Definition{ID: SeederID, Run: func(ctx context.Context, tx *database.Tx) error { _, err := SeedIn(ctx, tx, store); return err }}
}

// Seed seeds country reference data in a store write transaction.
func Seed(ctx context.Context, store *extensions.Store) (SeedResult, error) {
	var result SeedResult
	err := store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = seedReference(ctx, tx, store)
		return err
	})
	if err != nil {
		return SeedResult{}, err
	}
	return result, nil
}

// SeedIn joins the caller's exact pool/transaction under an isolated savepoint.
// A successful result is provisional until the parent transaction commits.
func SeedIn(ctx context.Context, tx *database.Tx, store *extensions.Store) (SeedResult, error) {
	var result SeedResult
	err := store.Join(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = seedReference(ctx, tx, store)
		return err
	})
	if err != nil {
		return SeedResult{}, err
	}
	return result, nil
}
func seedReference(ctx context.Context, tx *database.Tx, store *extensions.Store) (SeedResult, error) {
	rows, err := loadReference()
	if err != nil {
		return SeedResult{}, err
	}
	now, err := store.Now()
	if err != nil {
		return SeedResult{}, err
	}
	drafts := make([][]orm.Assignment[Country], len(rows))
	for i, row := range rows {
		if err := ctx.Err(); err != nil {
			return SeedResult{}, err
		}
		draft, err := referenceDraft(row, now)
		if err != nil {
			return SeedResult{}, err
		}
		drafts[i] = draft
	}
	f := Countries
	conflict := orm.OnConflict(orm.ConflictKey(f.ISO2)).DoUpdate(orm.ConflictIncoming(f.ISO3), orm.ConflictIncoming(f.ISONumeric), orm.ConflictIncoming(f.Name), orm.ConflictIncoming(f.OfficialName), orm.ConflictIncoming(f.Capital), orm.ConflictIncoming(f.Region), orm.ConflictIncoming(f.Subregion), orm.ConflictIncoming(f.Currencies), orm.ConflictIncoming(f.PrimaryCurrencyCode), orm.ConflictIncoming(f.CallingCode), orm.ConflictIncoming(f.CallingRoot), orm.ConflictIncoming(f.CallingSuffixes), orm.ConflictIncoming(f.TLDs), orm.ConflictIncoming(f.Timezones), orm.ConflictIncoming(f.Latitude), orm.ConflictIncoming(f.Longitude), orm.ConflictIncoming(f.Independent), orm.ConflictIncoming(f.UNMember), orm.ConflictIncoming(f.FlagEmoji), orm.ConflictIncoming(f.ReferenceVersion), orm.ConflictIncoming(f.UpdatedAt))
	stored, err := orm.UpsertMany(ctx, tx, Countries.Query(), conflict, drafts)
	if err != nil {
		return SeedResult{}, err
	}
	if len(stored) != len(rows) {
		return SeedResult{}, invalid()
	}
	return SeedResult{Version: BuiltinVersion, Rows: len(stored)}, nil
}
func referenceDraft(r reference, now temporal.DateTime) ([]orm.Assignment[Country], error) {
	fields := Countries
	currencies, err := value.NewJSON(r.Currencies)
	if err != nil {
		return nil, err
	}
	suffixes, err := value.NewJSON(r.CallingSuffixes)
	if err != nil {
		return nil, err
	}
	tlds, err := value.NewJSON(r.TLDs)
	if err != nil {
		return nil, err
	}
	zones, err := value.NewJSON(r.Timezones)
	if err != nil {
		return nil, err
	}
	d := []orm.Assignment[Country]{
		orm.Set(fields.ISO2, r.ISO2),
		orm.Set(fields.ISO3, r.ISO3),
		orm.Set(fields.Name, strings.TrimSpace(r.Name)),
		orm.Set(fields.Currencies, currencies),
		orm.Set(fields.CallingSuffixes, suffixes),
		orm.Set(fields.TLDs, tlds),
		orm.Set(fields.Timezones, zones),
		orm.Set(fields.Status, DisabledStatus),
		orm.SetNull(fields.ConversionRate),
		orm.Set(fields.IsDefault, false),
		orm.Set(fields.ReferenceVersion, BuiltinVersion),
		orm.Set(fields.CreatedAt, now),
		orm.Set(fields.UpdatedAt, now),
	}
	d = optional(d, trimmed(r.ISONumeric), func(v string) orm.Assignment[Country] { return orm.Set(fields.ISONumeric, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.ISONumeric) })
	d = optional(d, trimmed(r.OfficialName), func(v string) orm.Assignment[Country] { return orm.Set(fields.OfficialName, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.OfficialName) })
	d = optional(d, trimmed(r.Capital), func(v string) orm.Assignment[Country] { return orm.Set(fields.Capital, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.Capital) })
	d = optional(d, trimmed(r.Region), func(v string) orm.Assignment[Country] { return orm.Set(fields.Region, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.Region) })
	d = optional(d, trimmed(r.Subregion), func(v string) orm.Assignment[Country] { return orm.Set(fields.Subregion, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.Subregion) })
	d = optional(d, trimmed(r.PrimaryCurrencyCode), func(v string) orm.Assignment[Country] { return orm.Set(fields.PrimaryCurrencyCode, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.PrimaryCurrencyCode) })
	d = optional(d, trimmed(r.CallingCode), func(v string) orm.Assignment[Country] { return orm.Set(fields.CallingCode, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.CallingCode) })
	d = optional(d, trimmed(r.CallingRoot), func(v string) orm.Assignment[Country] { return orm.Set(fields.CallingRoot, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.CallingRoot) })
	d = optional(d, trimmed(r.FlagEmoji), func(v string) orm.Assignment[Country] { return orm.Set(fields.FlagEmoji, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.FlagEmoji) })
	d = optional(d, r.Latitude, func(v float64) orm.Assignment[Country] { return orm.Set(fields.Latitude, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.Latitude) })
	d = optional(d, r.Longitude, func(v float64) orm.Assignment[Country] { return orm.Set(fields.Longitude, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.Longitude) })
	d = optional(d, r.Independent, func(v bool) orm.Assignment[Country] { return orm.Set(fields.Independent, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.Independent) })
	d = optional(d, r.UNMember, func(v bool) orm.Assignment[Country] { return orm.Set(fields.UNMember, v) }, func() orm.Assignment[Country] { return orm.SetNull(fields.UNMember) })
	return d, nil
}
func optional[T any](d []orm.Assignment[Country], input *T, set func(T) orm.Assignment[Country], clear func() orm.Assignment[Country]) []orm.Assignment[Country] {
	if input == nil {
		return append(d, clear())
	}
	return append(d, set(*input))
}
func trimmed(input *string) *string {
	if input == nil {
		return nil
	}
	text := strings.TrimSpace(*input)
	if text == "" {
		return nil
	}
	return &text
}
