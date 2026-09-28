package translations

import (
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresTranslationCleanupExceedsPerModelWriteBatch(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	catalog, err := i18n.NewLocaleSet("en", "en", "fr", "de", "it", "es", "ms", "zh", "ja", "ko", "ru", "ar", "pt", "nl", "pl", "th", "vi", "id")
	if err != nil {
		t.Fatal(err)
	}
	fields := make([]Field[extensiontest.Member, int64], MaxFieldsPerOwner)
	registrations := make([]Registration, len(fields))
	for i := range fields {
		fields[i] = Define(extensiontest.Members, Name(fmt.Sprintf("field.%02d", i)), Options{})
		registrations[i] = fields[i].Registration()
	}
	manager, err := New(fixture.Store, catalog, registrations...)
	if err != nil {
		t.Fatal(err)
	}
	owner := extensiontest.Members.Reference(1)
	assignments := make([]Assignment[extensiontest.Member, int64], 0, len(fields)*len(catalog.Locales()))
	for _, field := range fields {
		for _, locale := range catalog.Locales() {
			assignments = append(assignments, field.SetValue(locale, "retained text"))
		}
	}
	for start := 0; start < len(assignments); start += MaxAssignments {
		if err := Set(t.Context(), manager, owner, assignments[start:min(start+MaxAssignments, len(assignments))]...); err != nil {
			t.Fatal(err)
		}
	}
	count, err := DeleteAll(t.Context(), manager, extensiontest.Members, owner)
	if err != nil || count != len(assignments) || count <= 1000 {
		t.Fatal("cleanup did not handle full translation budget", count, err)
	}
	if rows, err := All(t.Context(), manager, extensiontest.Members, owner); err != nil || len(rows) != 0 {
		t.Fatal("cleanup left rows", err)
	}
}
