package localization_test

import (
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/collection"
	"github.com/weiloon1234/Foundry-Go/randomtoken"
	"github.com/weiloon1234/Foundry-Go/sanitize"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type consumerClock struct{}

func (consumerClock) Now() time.Time { return time.Date(2026, 9, 17, 20, 0, 0, 0, time.UTC) }

func TestSupportingPublicAPIsRetainOrdinaryGoValues(t *testing.T) {
	token, err := randomtoken.Base64(32)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token.Reveal())
	if err != nil || len(raw) != 32 {
		t.Fatal(err)
	}
	policy, err := sanitize.New(sanitize.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := policy.HTML(t.Context(), `<p>safe</p><script>unsafe()</script>`)
	if err != nil || fragment != "<p>safe</p>" {
		t.Fatal(fragment, err)
	}
	lengths := collection.Map([]string{"one", "four"}, func(s string) int { return len(s) })
	if !slices.Equal(lengths, []int{3, 4}) {
		t.Fatal(lengths)
	}
	groups := collection.GroupBy([]string{"one", "two", "four"}, func(s string) int { return len(s) })
	if !slices.Equal(groups[3], []string{"one", "two"}) {
		t.Fatal(groups)
	}
	zone := time.FixedZone("UTC+08:00", 8*3600)
	today, err := temporal.Today(consumerClock{}, zone)
	if err != nil || today.String() != "2026-09-18" {
		t.Fatal(today, err)
	}
}
