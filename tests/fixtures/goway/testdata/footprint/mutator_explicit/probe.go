package mutator_explicit

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
)

type Model struct {
	ID    int64
	Name  string
	Maybe value.Nullable[string]
}
type Fields struct {
	orm.Table[Model, int64]
	Name  orm.Text[Model, string]
	Maybe orm.NullableText[Model, string]
}

func normalize(v string) (string, error) { return strings.TrimSpace(v), nil }

var Models = orm.Define("explicit", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name, orm.Mutate(normalize)), orm.NullableTextOf(b, &m.Maybe, orm.Mutate(normalize))}
})
