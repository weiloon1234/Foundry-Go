package generate

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var updateDTOGoldens = flag.Bool("update-dto-goldens", false, "refresh generated row DTO golden output")

const dtoRowSource = `package sample

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:dto role=row
type MemberRow struct {
	ID        int64                  ` + "`json:\"id\"`" + `
	// Username is the sign-in name shown in the member report.
	Username  string                 ` + "`json:\"username\"`" + `
	Email     value.Nullable[string] ` + "`json:\"email\"`" + `
	CreatedAt temporal.DateTime      ` + "`json:\"created_at\"`" + `
}

//foundry:dto
type OrdinaryRequest struct {
	Username string ` + "`json:\"username\"`" + `
}
`

func TestDTORowColumnsGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, dtoRowSource)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	rowOut := first["member_row_foundry.gen.go"]
	for _, want := range []string{
		"func MemberRowJSON() foundrycontract.JSON[MemberRow]",
		"type MemberRowColumnSet struct {",
		"func MemberRowColumns() MemberRowColumnSet { return *foundryMemberRowColumns() }",
		"foundryvalidation.Binding[MemberRow, string]",
		`foundryvalidation.DefineBinding("username", func(input *MemberRow) *string {`,
		"return &input.Username",
		// The DTO field's documentation reaches hover and go doc on the binding.
		"// Username binds the declared JSON property \"username\".\n\t//\n\t// Username is the sign-in name shown in the member report.\n\t//\n\tUsername foundryvalidation.Binding[MemberRow, string]",
	} {
		if !strings.Contains(rowOut, want) {
			t.Fatalf("missing %q in generated row DTO:\n%s", want, rowOut)
		}
	}
	// role=row must NOT emit ValidationFields or import any ORM/SQL package.
	for _, forbidden := range []string{"MemberRowValidationFields", "database/orm", "database/sql"} {
		if strings.Contains(rowOut, forbidden) {
			t.Fatalf("unexpected %q in generated row DTO:\n%s", forbidden, rowOut)
		}
	}
	// Ordinary //foundry:dto keeps ValidationFields unchanged.
	reqOut := first["ordinary_request_foundry.gen.go"]
	if !strings.Contains(reqOut, "OrdinaryRequestValidationFields") || strings.Contains(reqOut, "OrdinaryRequestColumns") {
		t.Fatalf("ordinary DTO output changed unexpectedly:\n%s", reqOut)
	}

	// Deterministic repeat generation and Check mode.
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("row DTO generation is not deterministic")
	}
	if _, err := generateFixture(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}

	// Verify generated code compiles and runtime pointer/name/read work.
	write(t, dir, "row_columns_runtime_test.go", `package sample_test
import (
	"testing"
	sample "foundry.test/generator"
	"github.com/weiloon1234/Foundry-Go/validation"
)
func TestGeneratedRowColumns(t *testing.T) {
	cols := sample.MemberRowColumns()
	if got := validation.BindingName(cols.Username); got != "username" {
		t.Fatalf("BindingName = %q, want username", got)
	}
	var row sample.MemberRow
	*validation.BoundPointer(cols.Username, &row) = "ada"
	if got := *validation.BoundPointer(cols.Username, &row); got != "ada" {
		t.Fatalf("*BoundPointer = %q, want ada", got)
	}
}
`)
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated row DTO test failed: %v\n%s", err, out)
	}
}

// TestDTORowColumnsGolden pins the complete generated output of a role=row DTO.
// The golden is written only by the generator: refresh it with -update-dto-goldens.
func TestDTORowColumnsGolden(t *testing.T) {
	dir := fixture(t, dtoRowSource)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	got := []byte(generatedSnapshot(t, dir)["member_row_foundry.gen.go"])
	path := filepath.Join("testdata", "dto", "member_row.golden")
	if *updateDTOGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generated row DTO differs from %s; refresh with -update-dto-goldens", path)
	}
}

func TestDTORowRoleRejectsGenericAndEmbedded(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			name: "generic",
			src:  "package sample\n//foundry:dto role=row\ntype GenericRow[T any] struct { Value T `json:\"value\"` }\n",
			want: "row DTO declarations cannot be generic",
		},
		{
			name: "embedded",
			src:  "package sample\ntype Base struct { Name string `json:\"name\"` }\n//foundry:dto role=row\ntype EmbeddedRow struct { Base }\n",
			want: "row DTO",
		},
		{
			name: "collision",
			src:  "package sample\n//foundry:dto role=row\ntype ClashRow struct { Name string `json:\"name\"` }\nfunc ClashRowColumns() {}\n",
			want: "ClashRowColumns",
		},
		{
			name: "http_body",
			src:  "package sample\n//foundry:dto role=row http=body\ntype BodyRow struct { Name string `json:\"name\"` }\n",
			want: "row DTO declarations cannot use http=body or http=both",
		},
		{
			name: "http_both",
			src:  "package sample\n//foundry:dto role=row http=both\ntype BothRow struct { Name string `json:\"name\"` }\n",
			want: "row DTO declarations cannot use http=body or http=both",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, tc.src)
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}
