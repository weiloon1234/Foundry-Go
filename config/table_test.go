package config_test

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/secret"
	"strings"
	"testing"
	"time"
)

func TestNamedTableSchemaDefaultsAndRejection(t *testing.T) {
	type settings struct {
		Timeout  time.Duration
		Port     uint16
		Password secret.String
		Huge     uint64
	}
	schema, err := config.New(config.Duration("pool.timeout", func(s *settings) *time.Duration { return &s.Timeout }), config.Scalar("port", func(s *settings) *uint16 { return &s.Port }), config.Secret("password", func(s *settings) *secret.String { return &s.Password }), config.Scalar("huge", func(s *settings) *uint64 { return &s.Huge }))
	if err != nil {
		t.Fatal(err)
	}
	defaults := func(string) settings { return settings{Timeout: time.Minute, Port: 5432} }
	values, err := config.DecodeTable(`{"one":{"pool":{"timeout":"2s"},"password":"private","huge":18446744073709551615},"two":{}}`, schema, defaults, nil)
	if err != nil || len(values) != 2 || values["one"].Timeout != 2*time.Second || values["one"].Huge != ^uint64(0) || values["two"].Port != 5432 || values["one"].Password.Reveal() != "private" {
		t.Fatal("table decoding failed", err)
	}
	for _, raw := range []string{`null`, `[]`, `{"a":null}`, `{"a":{},"a":{}}`, `{"a":{"pool":{"timeout":"2s","timeout":"3s"}}}`, `{"a":{"unknown":1}}`, `{"a":{"port":65536}}`, `{"a":{"pool":{"timeout":"private"}}}`, `{"bad name":{}}`, `{} {}`, strings.Repeat(" ", config.MaxTableBytes+1)} {
		result, err := config.DecodeTable(raw, schema, defaults, nil)
		if err == nil || result != nil {
			t.Fatalf("invalid table accepted: %d bytes", len(raw))
		}
		if strings.Contains(err.Error(), "private") {
			t.Fatal("secret error leaked")
		}
	}
}
