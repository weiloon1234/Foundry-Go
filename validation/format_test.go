package validation_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestFormatRulesUseDeclaredParserSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		rule           validation.Rule[string]
		valid, invalid []string
	}{
		{"email", validation.Email[string](), []string{"member@example.test", `"quoted local"@example.test`}, []string{"", " Member@example.test", "Member <member@example.test>", "<member@example.test>", "member(comment)@example.test", "a@example.test,b@example.test"}},
		{"url", validation.URL[string](), []string{"https://example.test/path?q=x", "http://127.0.0.1:8080/"}, []string{"/relative", "javascript:alert(1)", "https:///missing-host", "https://example.test:bad/"}},
		{"ip", validation.IP[string](), []string{"127.0.0.1", "2001:db8::1"}, []string{"example.test", "127.0.0.1/24", "fe80::1%eth0"}},
		{"ipv4", validation.IPv4[string](), []string{"127.0.0.1"}, []string{"::1", "127.0.0.999"}},
		{"ipv6", validation.IPv6[string](), []string{"::1", "::ffff:127.0.0.1"}, []string{"127.0.0.1", "::1%lo0"}},
		{"uuid", validation.UUID[string](), []string{"0193fd8c-2075-7000-8000-000000000001", "00000000-0000-0000-0000-000000000000"}, []string{"0193fd8c207570008000000000000001", "{0193fd8c-2075-7000-8000-000000000001}"}},
		{"date", validation.Date[string](), []string{"2024-02-29"}, []string{"2025-02-29", "2024-2-9"}},
		{"time", validation.Time[string](), []string{"00:00:00", "23:59:59.123"}, []string{"24:00:00", "23:59:60"}},
		{"datetime", validation.DateTime[string](), []string{"2024-02-29T12:30:00+08:00"}, []string{"2024-02-29T12:30:00"}},
		{"local", validation.LocalDateTime[string](), []string{"2024-02-29T12:30:00"}, []string{"2024-02-29T12:30:00Z"}},
		{"json", validation.JSON[string](), []string{`{"x":1}`, `[1,"two",null]`}, []string{`{"x":1,"x":2}`, `[NaN]`, `{} {}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, input := range tc.valid {
				if err := tc.rule.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
					t.Fatalf("valid input %q: %v", input, err)
				}
			}
			for _, input := range tc.invalid {
				rejection(t, tc.rule.Check(t.Context(), input, validation.DefaultLimits()))
			}
		})
	}
}

func TestFormatRulesKeepTextBoundsAndServerMetadata(t *testing.T) {
	t.Parallel()
	limits := validation.DefaultLimits()
	limits.ValueBytes = 4
	var bound *validation.LimitError
	if err := validation.Email[string]().Check(t.Context(), "a@example.test", limits); !errors.As(err, &bound) {
		t.Fatal("format bypassed text bound", err)
	}
	for _, rule := range []validation.Rule[string]{validation.Email[string](), validation.URL[string](), validation.JSON[string]()} {
		info, err := rule.Description()
		if err != nil || !info.ServerOnly {
			t.Fatal("parser behavior was promised to browsers")
		}
	}
}
