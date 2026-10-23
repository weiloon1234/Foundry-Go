package column_interval

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type Model struct {
	ID    int64
	Value temporal.Interval
}
type Fields struct {
	orm.Table[Model, int64]
	Value orm.Interval[Model]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.IntervalOf(b, &m.Value)}
})
