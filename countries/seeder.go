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
		Countries.ISO2.Set(r.ISO2),
		Countries.ISO3.Set(r.ISO3),
		Countries.Name.Set(strings.TrimSpace(r.Name)),
		Countries.Currencies.Set(currencies),
		Countries.CallingSuffixes.Set(suffixes),
		Countries.TLDs.Set(tlds),
		Countries.Timezones.Set(zones),
		Countries.Status.Set(DisabledStatus),
		Countries.ConversionRate.SetNull(),
		Countries.IsDefault.Set(false),
		Countries.ReferenceVersion.Set(BuiltinVersion),
		Countries.CreatedAt.Set(now),
		Countries.UpdatedAt.Set(now),
	}
	d = optional(d, trimmed(r.ISONumeric), Countries.ISONumeric.Set, Countries.ISONumeric.SetNull)
	d = optional(d, trimmed(r.OfficialName), Countries.OfficialName.Set, Countries.OfficialName.SetNull)
	d = optional(d, trimmed(r.Capital), Countries.Capital.Set, Countries.Capital.SetNull)
	d = optional(d, trimmed(r.Region), Countries.Region.Set, Countries.Region.SetNull)
	d = optional(d, trimmed(r.Subregion), Countries.Subregion.Set, Countries.Subregion.SetNull)
	d = optional(d, trimmed(r.PrimaryCurrencyCode), Countries.PrimaryCurrencyCode.Set, Countries.PrimaryCurrencyCode.SetNull)
	d = optional(d, trimmed(r.CallingCode), Countries.CallingCode.Set, Countries.CallingCode.SetNull)
	d = optional(d, trimmed(r.CallingRoot), Countries.CallingRoot.Set, Countries.CallingRoot.SetNull)
	d = optional(d, trimmed(r.FlagEmoji), Countries.FlagEmoji.Set, Countries.FlagEmoji.SetNull)
	d = optional(d, r.Latitude, Countries.Latitude.Set, Countries.Latitude.SetNull)
	d = optional(d, r.Longitude, Countries.Longitude.Set, Countries.Longitude.SetNull)
	d = optional(d, r.Independent, Countries.Independent.Set, Countries.Independent.SetNull)
	d = optional(d, r.UNMember, Countries.UNMember.Set, Countries.UNMember.SetNull)
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
