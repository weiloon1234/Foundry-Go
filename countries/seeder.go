package countries

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	drafts := make([]CountryDraft, len(rows))
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
	f := CountryFields()
	conflict := query.OnConflict(f.ISO2).Update(f.ISO3, f.ISONumeric, f.Name, f.OfficialName, f.Capital, f.Region, f.Subregion, f.Currencies, f.PrimaryCurrencyCode, f.CallingCode, f.CallingRoot, f.CallingSuffixes, f.TLDs, f.Timezones, f.Latitude, f.Longitude, f.Independent, f.UNMember, f.FlagEmoji, f.ReferenceVersion, f.UpdatedAt)
	stored, err := QueryFoundryCountries().UpsertMany(ctx, tx, drafts, conflict)
	if err != nil {
		return SeedResult{}, err
	}
	if len(stored) != len(rows) {
		return SeedResult{}, invalid()
	}
	return SeedResult{Version: BuiltinVersion, Rows: len(stored)}, nil
}
func referenceDraft(r reference, now temporal.DateTime) (CountryDraft, error) {
	currencies, err := value.NewJSON(r.Currencies)
	if err != nil {
		return CountryDraft{}, err
	}
	suffixes, err := value.NewJSON(r.CallingSuffixes)
	if err != nil {
		return CountryDraft{}, err
	}
	tlds, err := value.NewJSON(r.TLDs)
	if err != nil {
		return CountryDraft{}, err
	}
	zones, err := value.NewJSON(r.Timezones)
	if err != nil {
		return CountryDraft{}, err
	}
	d := CountryDraft{}.SetISO2(r.ISO2).SetISO3(r.ISO3).SetName(strings.TrimSpace(r.Name)).SetCurrencies(currencies).SetCallingSuffixes(suffixes).SetTLDs(tlds).SetTimezones(zones).SetStatus(DisabledStatus).ClearConversionRate().SetIsDefault(false).SetReferenceVersion(BuiltinVersion).SetCreatedAt(now).SetUpdatedAt(now)
	d = optional(d, trimmed(r.ISONumeric), CountryDraft.SetISONumeric, CountryDraft.ClearISONumeric)
	d = optional(d, trimmed(r.OfficialName), CountryDraft.SetOfficialName, CountryDraft.ClearOfficialName)
	d = optional(d, trimmed(r.Capital), CountryDraft.SetCapital, CountryDraft.ClearCapital)
	d = optional(d, trimmed(r.Region), CountryDraft.SetRegion, CountryDraft.ClearRegion)
	d = optional(d, trimmed(r.Subregion), CountryDraft.SetSubregion, CountryDraft.ClearSubregion)
	d = optional(d, trimmed(r.PrimaryCurrencyCode), CountryDraft.SetPrimaryCurrencyCode, CountryDraft.ClearPrimaryCurrencyCode)
	d = optional(d, trimmed(r.CallingCode), CountryDraft.SetCallingCode, CountryDraft.ClearCallingCode)
	d = optional(d, trimmed(r.CallingRoot), CountryDraft.SetCallingRoot, CountryDraft.ClearCallingRoot)
	d = optional(d, trimmed(r.FlagEmoji), CountryDraft.SetFlagEmoji, CountryDraft.ClearFlagEmoji)
	d = optional(d, r.Latitude, CountryDraft.SetLatitude, CountryDraft.ClearLatitude)
	d = optional(d, r.Longitude, CountryDraft.SetLongitude, CountryDraft.ClearLongitude)
	d = optional(d, r.Independent, CountryDraft.SetIndependent, CountryDraft.ClearIndependent)
	d = optional(d, r.UNMember, CountryDraft.SetUNMember, CountryDraft.ClearUNMember)
	return d, nil
}
func optional[T any](d CountryDraft, input *T, set func(CountryDraft, T) CountryDraft, clear func(CountryDraft) CountryDraft) CountryDraft {
	if input == nil {
		return clear(d)
	}
	return set(d, *input)
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
