package expr

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type constantOwner struct {
	ID    int64
	Label value.Nullable[string]
}

func TestConstantBindingAndRedaction(t *testing.T) {
	input := []byte("constant-private-sentinel")
	e := Constant[constantOwner](codec.Bytes[[]byte](), input)
	input[0] = 'X'
	node := e.data.core.Node.(querycore.ParameterNode)
	if !reflect.DeepEqual(querycore.LiteralValue(node.Value), []byte("constant-private-sentinel")) {
		t.Fatal("constant did not snapshot input")
	}
	for _, object := range []any{e, struct {
		private RowExpression[constantOwner, []byte]
	}{e}, struct{ nested any }{struct {
		private RowExpression[constantOwner, []byte]
	}{e}}} {
		for _, verb := range []string{"%v", "%+v", "%#v"} {
			for _, text := range []string{fmt.Sprintf(verb, object), fmt.Errorf(verb, object).Error()} {
				if strings.Contains(text, "constant-private-sentinel") || strings.Contains(text, "99 111 110 115 116 97 110 116") {
					t.Fatal("constant leaked")
				}
			}
		}
	}
	if !e.data.core.ReferenceFree || e.data.core.Err != nil {
		t.Fatal("invalid constant", e.data.core.Err)
	}
	if Constant[constantOwner](codec.Codec[string]{}, "x").data.core.Err == nil {
		t.Fatal("invalid codec accepted")
	}
	rejected := errors.New("constant codec rejected input")
	checked := codec.String[string]().Validated(func(string) error { return rejected })
	if err := Constant[constantOwner](checked, "private-value").data.core.Err; !errors.Is(err, rejected) {
		t.Fatal("constant ignored codec validation", err)
	}
	nullable := Constant[constantOwner](codec.Nullable(codec.String[string]()), value.Null[string]())
	if nullable.data.core.Err != nil || !nullable.data.column.Nullable {
		t.Fatal("nullable constant changed")
	}
	if querycore.LiteralValue(nullable.data.core.Node.(querycore.ParameterNode).Value) != nil {
		t.Fatal("NULL became a value")
	}
}

func TestRowPreservesProjectionAndScope(t *testing.T) {
	type fields struct {
		orm.Table[constantOwner, int64]
		Label orm.NullableText[constantOwner, string]
	}
	table := orm.Define("constant_owners", func(b orm.Builder[constantOwner], m *constantOwner) fields {
		return fields{orm.TableOf(b, &m.ID), orm.NullableTextOf(b, &m.Label)}
	})
	scoped := Value(table.Label)
	var row RowExpression[constantOwner, value.Nullable[string]] = Row(scoped)
	if !reflect.DeepEqual(row.data.core, scoped.core) {
		t.Fatal("row changed expression")
	}
	param := row.Param(value.Null[string]())
	if param.data.core.Err != nil || !param.data.column.Nullable {
		t.Fatal("projection type/codec lost")
	}
	if Row(Expression[constantOwner, string, string]{}).data.core.Err == nil {
		t.Fatal("zero expression accepted")
	}
}
