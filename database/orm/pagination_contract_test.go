package orm_test

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPaginationContractsShareOneOwner(t *testing.T) {
	for _, pair := range [][2]reflect.Type{
		{reflect.TypeFor[orm.PageRequest](), reflect.TypeFor[querycore.PageRequest]()},
		{reflect.TypeFor[orm.PageResult[int]](), reflect.TypeFor[querycore.Page[int]]()},
		{reflect.TypeFor[orm.SimplePage[int]](), reflect.TypeFor[querycore.SimplePage[int]]()},
		{reflect.TypeFor[orm.Cursor[int]](), reflect.TypeFor[querycore.Cursor[int]]()},
		{reflect.TypeFor[orm.CursorRequest[int]](), reflect.TypeFor[querycore.CursorRequest[int]]()},
		{reflect.TypeFor[orm.CursorPage[int]](), reflect.TypeFor[querycore.CursorPage[int]]()},
		{reflect.TypeFor[orm.CursorInputError](), reflect.TypeFor[querycore.CursorInputError]()},
	} {
		if pair[0] != pair[1] || pair[0].PkgPath() != "github.com/weiloon1234/Foundry-Go/internal/querycore" {
			t.Fatalf("pagination contract has multiple owners: %v", pair)
		}
	}
	if orm.MaxPageSize != 1000 || querycore.MaxPageSize != orm.MaxPageSize || querycore.MaxCursorBytes != 64*1024 || querycore.MaxCursorFields != 16 {
		t.Fatal("pagination bounds changed")
	}
	for _, request := range []orm.PageRequest{{}, {Number: 1, Size: 1001}, {Number: math.MaxInt, Size: 2}} {
		if !errors.Is(request.Validate(), fault.Invalid) {
			t.Fatal("invalid page request accepted")
		}
	}
	if err := (orm.PageRequest{Number: math.MaxInt, Size: 1}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPaginationContractBytesAndRedaction(t *testing.T) {
	// Frozen v1 envelope: exact integers, JSON field order and raw URL encoding.
	const envelope = `{"v":1,"scope":"0000000000000000000000000000000000000000000000000000000000000000","values":[{"k":"int","t":"9223372036854775807"},{"k":"null"},{"k":"string","t":"private"}]}`
	want := base64.RawURLEncoding.EncodeToString([]byte(envelope))
	token, err := querycore.MakeCursor(strings.Repeat("0", 64), []driver.Value{int64(math.MaxInt64), nil, "private"})
	if err != nil || token != want {
		t.Fatal("cursor v1 bytes changed", err)
	}
	current, err := orm.ParseCursor[int](token)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := querycore.ParseCursor[int](token)
	if err != nil || current != owner || current.Token() != want {
		t.Fatal("cursor aliases changed token bytes", err)
	}
	for _, input := range []any{current, owner} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, input)
			if strings.Contains(text, token) || strings.Contains(text, "private") {
				t.Fatal("cursor formatting leaked values")
			}
		}
	}
	for _, tc := range []struct {
		input any
		want  string
	}{
		{orm.PageRequest{Number: 2, Size: 3}, `{"Number":2,"Size":3}`},
		{orm.PageResult[int]{Items: []int{7}, Number: 2, Size: 3, Total: 4, Pages: 2}, `{"Items":[7],"Number":2,"Size":3,"Total":4,"Pages":2}`},
		{orm.SimplePage[int]{Items: []int{7}, Number: 2, Size: 1, HasMore: true}, `{"Items":[7],"Number":2,"Size":1,"HasMore":true}`},
		{current, `{}`},
		{orm.CursorRequest[int]{Size: 1, After: value.Set(current), Before: value.Set(current)}, `{"Size":1,"After":{},"Before":{}}`},
		{orm.CursorPage[int]{Items: []int{7}, Size: 1, Next: value.Set(current), Previous: value.Set(current)}, `{"Items":[7],"Size":1,"Next":{},"Previous":{}}`},
	} {
		got, err := json.Marshal(tc.input)
		if err != nil || string(got) != tc.want {
			t.Fatalf("pagination JSON changed: %s, %v", got, err)
		}
	}
	for _, bad := range []string{"", "private", strings.Repeat("a", querycore.MaxCursorBytes+1), base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(envelope, `"v":1`, `"v":2`, 1)))} {
		if _, err := orm.ParseCursor[int](bad); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed cursor accepted")
		}
	}
	request := orm.CursorRequest[int]{Size: 1, After: value.Set(orm.Cursor[int]{})}
	var ownerInput *querycore.CursorInputError
	if err := request.Validate(); !errors.As(err, &ownerInput) || !errors.Is(err, fault.Invalid) || strings.Contains(fmt.Sprintf("%#v", err), "private") {
		t.Fatal("cursor input classification or redaction changed")
	}
	var input *orm.CursorInputError
	if err := (orm.CursorPage[int]{Size: 1, Items: []int{7}, Next: value.Set(orm.Cursor[int]{})}).Validate(); err == nil || errors.As(err, &input) {
		t.Fatal("output metadata failure classified as input")
	}
}

func TestRowsAndExpressionsDoNotImportLegacyEngine(t *testing.T) {
	for _, dir := range []string{"rows", "expr"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range file.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if name == "github.com/weiloon1234/Foundry-Go/database/query" {
					t.Errorf("%s imports the legacy engine", path)
				}
			}
		}
	}
}
