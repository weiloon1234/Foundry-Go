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

type SeedResult struct {
	Version string
	Rows    int
}

// Seeder plugs into the ordinary explicit transactional seeder registry. It
// never installs itself, migrates tables, fetches data or runs during app boot.
func Seeder(store *extensions.Store) seed.Definition {
	return seed.Definition{ID: SeederID, Run: func(ctx context.Context, tx *database.Tx) error { _, err := SeedIn(ctx, tx, store); return err }}
}
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
	conflict := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.ISO2))).DoUpdate(orm.ConflictIncoming(orm.TextExpr(f.ISO3)), orm.ConflictIncoming(orm.NullableTextExpr(f.ISONumeric)), orm.ConflictIncoming(orm.TextExpr(f.Name)), orm.ConflictIncoming(orm.NullableTextExpr(f.OfficialName)), orm.ConflictIncoming(orm.NullableTextExpr(f.Capital)), orm.ConflictIncoming(orm.NullableTextExpr(f.Region)), orm.ConflictIncoming(orm.NullableTextExpr(f.Subregion)), orm.ConflictIncoming(orm.JSONExpr(f.Currencies)), orm.ConflictIncoming(orm.NullableTextExpr(f.PrimaryCurrencyCode)), orm.ConflictIncoming(orm.NullableTextExpr(f.CallingCode)), orm.ConflictIncoming(orm.NullableTextExpr(f.CallingRoot)), orm.ConflictIncoming(orm.JSONExpr(f.CallingSuffixes)), orm.ConflictIncoming(orm.JSONExpr(f.TLDs)), orm.ConflictIncoming(orm.JSONExpr(f.Timezones)), orm.ConflictIncoming(orm.NullableNumberExpr(f.Latitude)), orm.ConflictIncoming(orm.NullableNumberExpr(f.Longitude)), orm.ConflictIncoming(orm.NullableColumnExpr(f.Independent)), orm.ConflictIncoming(orm.NullableColumnExpr(f.UNMember)), orm.ConflictIncoming(orm.NullableTextExpr(f.FlagEmoji)), orm.ConflictIncoming(orm.TextExpr(f.ReferenceVersion)), orm.ConflictIncoming(orm.TimeExpr(f.UpdatedAt)))
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
		orm.KeySet(fields.ISO2, r.ISO2),
		orm.TextSet(fields.ISO3, r.ISO3),
		orm.TextSet(fields.Name, strings.TrimSpace(r.Name)),
		orm.JSONSet(fields.Currencies, currencies),
		orm.JSONSet(fields.CallingSuffixes, suffixes),
		orm.JSONSet(fields.TLDs, tlds),
		orm.JSONSet(fields.Timezones, zones),
		orm.EnumSet(fields.Status, DisabledStatus),
		orm.NullableColumnSetNull(fields.ConversionRate),
		orm.ColumnSet(fields.IsDefault, false),
		orm.TextSet(fields.ReferenceVersion, BuiltinVersion),
		orm.TimeSet(fields.CreatedAt, now),
		orm.TimeSet(fields.UpdatedAt, now),
	}
	d = optional(d, trimmed(r.ISONumeric), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.ISONumeric, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.ISONumeric) })
	d = optional(d, trimmed(r.OfficialName), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.OfficialName, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.OfficialName) })
	d = optional(d, trimmed(r.Capital), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.Capital, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.Capital) })
	d = optional(d, trimmed(r.Region), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.Region, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.Region) })
	d = optional(d, trimmed(r.Subregion), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.Subregion, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.Subregion) })
	d = optional(d, trimmed(r.PrimaryCurrencyCode), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.PrimaryCurrencyCode, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.PrimaryCurrencyCode) })
	d = optional(d, trimmed(r.CallingCode), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.CallingCode, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.CallingCode) })
	d = optional(d, trimmed(r.CallingRoot), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.CallingRoot, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.CallingRoot) })
	d = optional(d, trimmed(r.FlagEmoji), func(v string) orm.Assignment[Country] { return orm.NullableTextSet(fields.FlagEmoji, v) }, func() orm.Assignment[Country] { return orm.NullableTextSetNull(fields.FlagEmoji) })
	d = optional(d, r.Latitude, func(v float64) orm.Assignment[Country] { return orm.NullableNumberSet(fields.Latitude, v) }, func() orm.Assignment[Country] { return orm.NullableNumberSetNull(fields.Latitude) })
	d = optional(d, r.Longitude, func(v float64) orm.Assignment[Country] { return orm.NullableNumberSet(fields.Longitude, v) }, func() orm.Assignment[Country] { return orm.NullableNumberSetNull(fields.Longitude) })
	d = optional(d, r.Independent, func(v bool) orm.Assignment[Country] { return orm.NullableColumnSet(fields.Independent, v) }, func() orm.Assignment[Country] { return orm.NullableColumnSetNull(fields.Independent) })
	d = optional(d, r.UNMember, func(v bool) orm.Assignment[Country] { return orm.NullableColumnSet(fields.UNMember, v) }, func() orm.Assignment[Country] { return orm.NullableColumnSetNull(fields.UNMember) })
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
