package http_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestSerializedURLMetadataRetainsSyntaxAndOwnsCases(t *testing.T) {
	info := foundryhttp.URLScalarInfo{Syntax: foundryhttp.EnumURLSyntax, Value: contract.Type{ID: "State", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"ready"`)}}}
	copy, err := info.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	copy.Value.Cases[0][1] = 'x'
	if string(info.Value.Cases[0]) != `"ready"` {
		t.Fatal("cases alias input")
	}
	for _, syntax := range []foundryhttp.URLSyntax{foundryhttp.IntegerURLSyntax, foundryhttp.ModelIDURLSyntax, foundryhttp.BooleanURLSyntax, "unknown"} {
		info.Syntax = syntax
		if _, err := info.Normalize(); err == nil {
			t.Fatal("inconsistent syntax accepted")
		}
	}
	info.Syntax = foundryhttp.CustomURLSyntax
	if normalized, err := info.Normalize(); err != nil || normalized.Syntax != foundryhttp.CustomURLSyntax {
		t.Fatal("custom syntax was inferred as native", err)
	}
}
