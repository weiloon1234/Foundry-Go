package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresListPageContainsUpgradeErrorMatching(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	old := Define("hostile.value", 1, contract.StringJSON[string](), Presentation{})
	original, err := New(f.Store, old.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Create(t.Context(), original, "stored"); err != nil {
		t.Fatal(err)
	}
	next := Define("hostile.value", 2, contract.StringJSON[string](), Presentation{})
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		calls := 0
		upgrade := UpgradeFrom(1, contract.StringJSON[string](), func(context.Context, string) (string, error) { calls++; return "", cause })
		manager, err := New(f.Store, next.RegistrationWith(Options[string]{Upgrades: []Upgrade[string]{upgrade}}))
		if err != nil {
			t.Fatal(err)
		}
		page, err := ListPage(t.Context(), manager, Filter{}, Cursor{}, 10)
		if err == nil || !errors.Is(err, cause) || len(page.Records) != 0 || !page.Next.IsZero() || calls != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 1024 {
			t.Fatal("failed upgrade completed a page or escaped containment")
		}
	}
}
