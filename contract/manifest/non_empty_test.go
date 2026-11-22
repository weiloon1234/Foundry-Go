package manifest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type NonEmptyManifestSKU string

func (s NonEmptyManifestSKU) MarshalJSON() ([]byte, error) { return json.Marshal(string(s)) }
func (s *NonEmptyManifestSKU) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*s = NonEmptyManifestSKU(text)
	return nil
}
func (NonEmptyManifestSKU) JSONContract() contract.JSON[NonEmptyManifestSKU] {
	typ := reflect.TypeFor[NonEmptyManifestSKU]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[NonEmptyManifestSKU](contract.Schema{
		Root:  id,
		Types: []contract.Type{{ID: id, Kind: contract.StringKind, NonEmpty: true}},
	})
}

type NonEmptyManifestBody struct {
	SKU  NonEmptyManifestSKU `json:"sku"`
	Note string              `json:"note"`
}

func nonEmptyManifestBodyJSON() contract.JSON[NonEmptyManifestBody] {
	skuType := reflect.TypeFor[NonEmptyManifestSKU]()
	skuID := contract.TypeID(skuType.PkgPath() + "." + skuType.Name())
	bodyType := reflect.TypeFor[NonEmptyManifestBody]()
	bodyID := contract.TypeID(bodyType.PkgPath() + "." + bodyType.Name())
	return contract.DefineJSON[NonEmptyManifestBody](contract.Schema{
		Root: bodyID,
		Types: []contract.Type{
			{ID: bodyID, Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "sku", Type: skuID, Required: true},
				{Name: "note", Type: "string", Required: true},
			}},
			contract.JSONType(skuID, (NonEmptyManifestSKU("")).JSONContract),
			{ID: "string", Kind: contract.StringKind},
		},
	})
}

// v11Type, v11JSONKeyInfo and v11Document reproduce the exact v0.50.0 (manifest
// version 11) serialized schema — every member of manifest.Document and
// contract.Type except contract.Type.NonEmpty — decoded with
// DisallowUnknownFields just as manifest.Decode does.
type v11JSONKeyInfo struct {
	Value      v11Type                `json:"value"`
	Syntax     contract.JSONKeySyntax `json:"syntax"`
	ServerOnly bool                   `json:"server_only"`
	NonZero    bool                   `json:"non_zero"`
}

type v11Type struct {
	ID            contract.TypeID     `json:"id"`
	Kind          contract.Kind       `json:"kind"`
	Nullable      bool                `json:"nullable"`
	Discriminator string              `json:"discriminator,omitempty"`
	Variants      []contract.Variant  `json:"variants,omitempty"`
	Properties    []contract.Property `json:"properties,omitempty"`
	Key           *v11JSONKeyInfo     `json:"key,omitempty"`
	Element       contract.TypeID     `json:"element,omitempty"`
	Length        value.Optional[int] `json:"length,omitzero"`
	Bits          uint8               `json:"bits,omitempty"`
	Signed        bool                `json:"signed,omitempty"`
	Format        contract.Format     `json:"format,omitempty"`
	Cases         []json.RawMessage   `json:"cases,omitempty"`
}

type v11Document struct {
	Version       int                           `json:"version"`
	Types         []v11Type                     `json:"types"`
	Roots         []contract.TypeID             `json:"roots"`
	HTTP          []manifest.Operation          `json:"http"`
	RawRoutes     []foundryhttp.RouteInfo       `json:"raw_routes,omitempty"`
	ErrorType     contract.TypeID               `json:"error_type"`
	Errors        []foundryhttp.ErrorDefinition `json:"errors"`
	Realtime      *manifest.Realtime            `json:"realtime,omitempty"`
	Notifications []manifest.Notification       `json:"notifications,omitempty"`
	Tables        []manifest.Table              `json:"tables,omitempty"`
	Locales       *manifest.Locales             `json:"locales,omitempty"`
	Enums         []enum.Definition             `json:"enums,omitempty"`
	Permissions   []auth.PermissionDescription  `json:"permissions,omitempty"`
	PublicConfig  []manifest.PublicSetting      `json:"public_config,omitempty"`
}

var errUnsupportedV11Version = errors.New("unsupported manifest version")

func decodeStrictV11(data []byte) (v11Document, error) {
	var doc v11Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return v11Document{}, err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return v11Document{}, errors.New("trailing manifest data")
	}
	if doc.Version != 11 {
		return v11Document{}, errUnsupportedV11Version
	}
	return doc, nil
}

// TestNonEmptyManifestVersionAndCrossVersionRejection proves with a faithful
// v11 reader that:
//  1. A valid v11 control document (same real builder output with version 11
//     and non_empty omitted) is ACCEPTED by the v11 reader and REJECTED by the
//     current v12 manifest.Decode.
//  2. The same document carrying non_empty (at version 11 or version 12) is
//     REJECTED by the strict v11 reader specifically on unknown field
//     "non_empty", and a non_empty-free v12 document is REJECTED by the v11
//     reader on unsupported manifest version.
//  3. The current v12 reader accepts v12 (round-tripping canonically) and
//     rejects v11, v13, and unknown fields.
func TestNonEmptyManifestVersionAndCrossVersionRejection(t *testing.T) {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "items.store", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/items"))
	ep := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(nonEmptyManifestBodyJSON()), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(ep.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, NonEmptyManifestBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != manifest.Version {
		t.Fatalf("manifest version = %d, want %d", doc.Version, manifest.Version)
	}
	if manifest.Version < 12 {
		t.Fatalf("manifest.Version = %d, want >= 12 for non_empty support", manifest.Version)
	}
	if !bytes.Contains(raw, []byte(`"non_empty": true`)) {
		t.Fatalf("manifest missing non_empty on SKU type:\n%s", raw)
	}

	// Build a faithful v11 control document from the real builder output:
	// identical roots, http, error_type, errors, and types, at version 11 with
	// non_empty omitted (the exact v0.50.0 shape).
	v11Control := bytes.Replace(raw, []byte(`"version": 12`), []byte(`"version": 11`), 1)
	v11Control = bytes.ReplaceAll(v11Control, []byte(",\n      \"non_empty\": true"), nil)
	if bytes.Contains(v11Control, []byte("non_empty")) {
		t.Fatalf("v11 control still contains non_empty:\n%s", v11Control)
	}

	// 1. Positive control: the faithful strict v11 reader MUST accept v11Control.
	parsedV11, err := decodeStrictV11(v11Control)
	if err != nil {
		t.Fatalf("faithful strict v11 reader rejected valid v11 control: %v", err)
	}
	if parsedV11.Version != 11 || len(parsedV11.HTTP) != 1 || parsedV11.ErrorType == "" || len(parsedV11.Errors) == 0 || len(parsedV11.Types) == 0 {
		t.Fatalf("v11 control lost required manifest members: version=%d http=%d error_type=%q errors=%d types=%d",
			parsedV11.Version, len(parsedV11.HTTP), parsedV11.ErrorType, len(parsedV11.Errors), len(parsedV11.Types))
	}

	// 2. Old-reader rejection of v12 with non_empty: must fail specifically on
	//    unknown field "non_empty" (not on missing top-level members).
	if _, err := decodeStrictV11(raw); err == nil || !strings.Contains(err.Error(), `unknown field "non_empty"`) {
		t.Fatalf("strict v11 reader on v12 with non_empty: got err=%v, want unknown field \"non_empty\"", err)
	}
	// Even if version were left at 11, non_empty still breaks the strict v11 reader.
	v11WithNonEmpty := bytes.Replace(raw, []byte(`"version": 12`), []byte(`"version": 11`), 1)
	if _, err := decodeStrictV11(v11WithNonEmpty); err == nil || !strings.Contains(err.Error(), `unknown field "non_empty"`) {
		t.Fatalf("strict v11 reader on v11 with non_empty: got err=%v, want unknown field \"non_empty\"", err)
	}
	// A v12 document with no non_empty types is rejected by the v11 reader on version.
	v12WithoutNonEmpty := bytes.ReplaceAll(raw, []byte(",\n      \"non_empty\": true"), nil)
	if _, err := decodeStrictV11(v12WithoutNonEmpty); !errors.Is(err, errUnsupportedV11Version) {
		t.Fatalf("strict v11 reader on non_empty-free v12: got err=%v, want unsupported manifest version", err)
	}

	// 3. Current v12 reader accepts v12 and round-trips canonically.
	roundTrip, err := manifest.Decode(raw)
	if err != nil {
		t.Fatalf("v12 Decode failed: %v", err)
	}
	again, err := roundTrip.JSON()
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatalf("v12 round-trip drifted: %v", err)
	}

	// 4. Current v12 reader rejects v11 (with or without non_empty), v13, and unknown fields.
	if _, err := manifest.Decode(v11Control); err == nil {
		t.Fatal("v12 reader accepted v11 control document")
	}
	if _, err := manifest.Decode(v11WithNonEmpty); err == nil {
		t.Fatal("v12 reader accepted v11 document with non_empty")
	}
	v13 := bytes.Replace(raw, []byte(`"version": 12`), []byte(`"version": 13`), 1)
	if _, err := manifest.Decode(v13); err == nil {
		t.Fatal("v12 reader accepted v13 document")
	}
	unknownField := bytes.Replace(raw, []byte(`"non_empty": true`), []byte(`"non_empty": true, "future_flag": true`), 1)
	if _, err := manifest.Decode(unknownField); err == nil {
		t.Fatal("v12 reader accepted unknown field")
	}
}
