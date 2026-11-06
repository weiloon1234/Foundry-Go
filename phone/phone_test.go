package phone_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/phone"
)

func TestParseRegions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		region    countries.Code
		raw, want string
	}{
		{"MY", "012-345 6789", "+60123456789"},
		{"MY", " +60 12 345 6789 ", "+60123456789"},
		{"SG", "9123 4567", "+6591234567"},
		{"SG", "+65 9123 4567", "+6591234567"},
		{"US", "(650) 253-0000", "+16502530000"},
		{"US", "+1 650 253 0000", "+16502530000"},
	} {
		t.Run(string(tc.region)+tc.raw, func(t *testing.T) {
			got, err := phone.Parse(tc.region, tc.raw, phone.Options{})
			if err != nil || got.String() != tc.want || got.IsZero() {
				t.Fatalf("parse = %q, %v", got.String(), err)
			}
			again, err := phone.Parse(tc.region, got.String(), phone.Options{})
			if err != nil || again != got {
				t.Fatalf("canonical round trip = %q, %v", again.String(), err)
			}
		})
	}
}

func TestParseRejectsInvalidInputWithoutDisclosingIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		region countries.Code
		raw    string
	}{
		{"short", "MY", "123"},
		{"text", "US", "private-invalid-phone"},
		{"wrong region", "MY", "+6591234567"},
		{"shared calling code wrong region", "US", "+1 416 555 0123"},
		{"extension", "MY", "0123456789 ext 7"},
		{"RFC3966 extension", "US", "tel:+1-650-253-0000;ext=123"},
		{"unknown region", "ZZ", "+16502530000"},
		{"empty region", "", "+16502530000"},
		{"lowercase region", "my", "0123456789"},
		{"empty", "MY", ""},
		{"blank", "SG", " \t\n"},
		{"over limit", "MY", strings.Repeat(" ", phone.MaxInputBytes) + "0123456789"},
		{"multibyte over limit", "MY", strings.Repeat("\u2000", 31) + "0123456789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := phone.Parse(tc.region, tc.raw, phone.Options{})
			if err == nil || !errors.Is(err, fault.Invalid) || !got.IsZero() {
				t.Fatalf("parse = %q, %v", got.String(), err)
			}
			if tc.raw != "" && strings.Contains(err.Error(), tc.raw) {
				t.Fatal("error disclosed input")
			}
		})
	}
}

func TestInputLimitCountsOriginalBytes(t *testing.T) {
	t.Parallel()
	for _, padding := range []string{" ", "\u2000"} {
		raw := strings.Repeat(padding, (phone.MaxInputBytes-10)/len(padding)) + "0123456789"
		if len(raw) != phone.MaxInputBytes {
			t.Fatal("fixture must reach byte limit")
		}
		if _, err := phone.Parse("MY", raw, phone.Options{}); err != nil {
			t.Fatal("rejected input at byte limit", err)
		}
		if _, err := phone.Parse("MY", raw+" ", phone.Options{}); !errors.Is(err, fault.Invalid) {
			t.Fatal("accepted input over byte limit", err)
		}
	}
}

func TestExtensionPolicy(t *testing.T) {
	t.Parallel()
	raw := "+1 650 253 0000 ext 123"
	if _, err := phone.Parse("US", raw, phone.Options{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("default accepted extension", err)
	}
	options := phone.Options{Extensions: phone.DiscardExtensions}
	got, err := phone.Parse("US", raw, options)
	if err != nil || got.String() != "+16502530000" {
		t.Fatalf("explicit discard = %q, %v", got.String(), err)
	}
	invalid := phone.Options{Extensions: phone.ExtensionPolicy(255)}
	if invalid.Validate() == nil {
		t.Fatal("unknown policy accepted")
	}
	if got, err := phone.Parse("US", raw, invalid); !errors.Is(err, fault.Invalid) || !got.IsZero() {
		t.Fatal("unknown policy parsed", err)
	}
	var zero phone.E164
	if !zero.IsZero() || zero.String() != "" {
		t.Fatal("zero number must be absent")
	}
}
