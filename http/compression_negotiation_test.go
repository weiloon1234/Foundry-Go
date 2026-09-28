package http

import (
	"strings"
	"testing"
)

func TestAcceptEncodingPreferenceAndBounds(t *testing.T) {
	config := DefaultCompressionConfig()
	for _, test := range []struct {
		lines    []string
		want     ContentEncoding
		identity bool
	}{
		{nil, "", true}, {[]string{""}, "", true}, {[]string{"gzip"}, GzipEncoding, true},
		{[]string{"br, gzip"}, BrotliEncoding, true},
		{[]string{"br;q=0.4, gzip;q=0.8"}, GzipEncoding, true},
		{[]string{"gzip;q=0.8, identity;q=1"}, "", true},
		{[]string{"gzip;q=0, *;q=1"}, BrotliEncoding, true},
		{[]string{"*;q=0"}, "", false},
		{[]string{"br;q=1, identity;q=0"}, BrotliEncoding, false},
		{[]string{"unsupported, gzip;q=0"}, "", true},
		{[]string{"gzip;Q=0.050", "br;q=0.025"}, GzipEncoding, true},
		{[]string{", , gzip, "}, GzipEncoding, true},
	} {
		p, err := parseAcceptEncoding(test.lines)
		if err != nil {
			t.Fatal(err)
		}
		encoder, ok := p.choose(config.Encoders)
		if (ok && encoder.Encoding() != test.want) || (!ok && test.want != "") || (p.quality("identity") > 0) != test.identity {
			t.Fatalf("input=%v selected=%s identity=%d", test.lines, encoder.Encoding(), p.quality("identity"))
		}
	}
	for _, input := range []string{
		"gzip;q=1.001", "gzip;q=-1", "gzip;q=.5", "gzip;q=2", "gzip;q=0.1234", "gzip;q=1e0",
		"gzip;q=NaN", "gzip;q=0.5;extra=1", "gzip;level=1", "gzip, GZIP", "gzip;q=0,gzip;q=1",
		"gzip\r\nInjected: true", "gzip\x7f", strings.Repeat(",", maxAcceptEncodingSlots),
		strings.Repeat("g", maxAcceptEncodingBytes+1),
	} {
		if _, err := parseAcceptEncoding([]string{input}); err == nil {
			t.Fatalf("invalid negotiation accepted: %q", input)
		}
	}
	if _, err := parseAcceptEncoding(make([]string, maxAcceptEncodingLines+1)); err == nil {
		t.Fatal("line limit ignored")
	}
	for _, value := range []string{"0.", "1.", "0.000", "0.001", "0.999", "1.000"} {
		if _, ok := parseCompressionQuality(value); !ok {
			t.Fatal("valid qvalue rejected")
		}
	}
}
func TestCompressionDeclarationOwnershipAndValidation(t *testing.T) {
	config := DefaultCompressionConfig()
	for _, bad := range []CompressionConfig{
		{}, {Encoders: config.Encoders, MinBytes: -1, MaxConcurrent: 1},
		{Encoders: config.Encoders, MinBytes: 65537, MaxConcurrent: 1},
		{Encoders: config.Encoders, MinBytes: 1, MaxConcurrent: 0},
		{Encoders: []CompressionEncoder{GzipCompression(GzipFastest), GzipCompression(GzipSmallest)}, MinBytes: 1, MaxConcurrent: 1},
		{Encoders: []CompressionEncoder{{}}, MinBytes: 1, MaxConcurrent: 1},
		{Encoders: []CompressionEncoder{GzipCompression(10)}, MinBytes: 1, MaxConcurrent: 1},
		{Encoders: []CompressionEncoder{BrotliCompression(12, 20)}, MinBytes: 1, MaxConcurrent: 1},
		{Encoders: []CompressionEncoder{BrotliCompression(4, 25)}, MinBytes: 1, MaxConcurrent: 1},
	} {
		if bad.Validate() == nil {
			t.Fatal("invalid compression config accepted")
		}
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}
func FuzzAcceptEncoding(f *testing.F) {
	f.Add("gzip, br;q=0.5")
	f.Add("identity;q=0, *;q=0")
	f.Add("gzip;q=0.1234")
	f.Fuzz(func(t *testing.T, raw string) {
		p, err := parseAcceptEncoding([]string{raw})
		if err != nil {
			return
		}
		if len(raw) > maxAcceptEncodingBytes {
			t.Fatal("oversized field accepted")
		}
		for _, q := range p.weights {
			if q < 0 || q > 1000 {
				t.Fatal("weight out of bounds")
			}
		}
	})
}
