package modelmeta

import (
	"strings"
	"testing"
)

func TestModelTagStorageAndDeclaration(t *testing.T) {
	got, err := ParseModelTag(`json:"name" foundry:"column=label,default=database,role=text,input=Name,mutate=normalize,codec=nameCodec,member=Label"`)
	if err != nil {
		t.Fatal(err)
	}
	if got.StorageTag() != "column=label,default=database" || got.Role != "text" || got.Input != "Name" || got.Mutate != "normalize" || got.Codec != "nameCodec" || got.Member != "Label" {
		t.Fatalf("wrong parsed tag: %+v", got)
	}
	for _, raw := range []string{"", `json:"name"`, `foundry:"slot"`, `foundry:"member=none"`, `foundry:"-"`, `foundry:"key-role=primary,member=ID"`} {
		if _, err := ParseModelTag(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	tags, err := ParseTags(`foundry:"role=text"`)
	if err != nil || tags["foundry"] != "role=text" {
		t.Fatal("ordinary tag parser must retain generator options", tags, err)
	}
}
func TestModelTagRejections(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`foundry:""`, "foundry tag cannot be empty"},
		{`foundry:"role=text,"`, "empty foundry option"},
		{`foundry:"-,role=text"`, "- must be the only foundry option"},
		{`foundry:"role=text,role=column"`, "duplicate foundry option role"},
		{`foundry:"slot=files"`, "slot is a bare flag, not slot=<name>"},
		{`foundry:"wat=x"`, "unknown foundry option wat"},
		{`foundry:"role"`, "foundry option requires key=value: role"},
		{`foundry:"role="`, "foundry option requires key=value: role"},
		{`foundry:"column=a-b"`, "column requires a SQL identifier"},
		{`foundry:"default=go"`, "default requires database"},
		{`foundry:"role=numeric"`, "unknown role numeric"},
		{`foundry:"key-role=other"`, "unknown key-role other"},
		{`foundry:"codec=foo()"`, "codec requires a named function"},
		{`foundry:"mutate=foo()"`, "mutate requires a named function"},
		{`foundry:"member=-"`, "member requires a Go identifier or none"},
		{`foundry:"member=_"`, "member requires a Go identifier or none"},
		{`foundry:"mutate=foo"`, "mutate requires input"},
		{`foundry:"values="`, "foundry option requires key=value: values"},
		{`foundry:"values=A||B"`, "values requires enum case names separated by |"},
		{`foundry:"values=A|B-C"`, "values requires enum case names separated by |"},
		{`foundry:"values=A|B|A"`, "values names A twice"},
		{`foundry:"values=A,values=B"`, "duplicate foundry option values"},
		{`foundry:"role=text`, "malformed quoted struct tag"},
		{`foundry:"role=text" foundry:"role=column"`, "duplicate struct tag namespace foundry"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			_, err := ParseModelTag(tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
	for _, key := range []string{"foudry", "foundy", "foundrry", "foundr", "foundyr", "Foundry", "xoundry", "afoundry", "foundry_model", "foundry-table", "foundry.ui", "foundry_ui"} {
		_, err := ParseModelTag(key + `:"column=name"`)
		if err == nil || !strings.Contains(err.Error(), "use foundry") {
			t.Errorf("near miss %s: %v", key, err)
		}
	}
	for _, key := range []string{"json", "xml", "db", "foundries", "other", "found", "boundary", "foundryui", "foudr", "founxxry", "founxyy"} {
		if _, err := ParseModelTag(key + `:"name"`); err != nil {
			t.Errorf("unrelated namespace %s: %v", key, err)
		}
	}
}

func TestModelTagValuesNarrowTheEnum(t *testing.T) {
	got, err := ParseModelTag(`foundry:"default=database,values=Draft|Published"`)
	if err != nil || strings.Join(got.Values, ",") != "Draft,Published" || got.StorageTag() != "default=database" {
		t.Fatalf("values: %+v %v", got, err)
	}
}

func TestModelInputTypeCommas(t *testing.T) {
	got, err := ParseModelTag(`foundry:"input=Pair[string,int],member=Value"`)
	if err != nil || got.Input != "Pair[string,int]" || got.Member != "Value" {
		t.Fatalf("generic type: %+v %v", got, err)
	}
	for _, raw := range []string{`foundry:"input=Pair[string,int"`, `foundry:"input=Pair[string,int)"`, `foundry:"input=Pair]"`} {
		if _, err := ParseModelTag(raw); err == nil || !strings.Contains(err.Error(), "unmatched delimiters") {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}
