package postgres

import (
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

type privateSQLValue struct{}

func (privateSQLValue) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "[SQL value]") }
func encodeContainer(m *pgtype.Map, codec pgtype.Codec, value any) any {
	switch codec.(type) {
	case *pgtype.ArrayCodec:
		if source, ok := value.(pgtype.ArrayGetter); ok {
			return arrayGetter{source: source, m: m}
		}
	case *pgtype.CompositeCodec:
		if source, ok := value.(pgtype.CompositeIndexGetter); ok {
			return compositeGetter{source: source, m: m}
		}
	case *pgtype.RangeCodec:
		if source, ok := value.(pgtype.RangeValuer); ok {
			return rangeGetter{source: source, m: m}
		}
	case *pgtype.MultirangeCodec:
		if source, ok := value.(pgtype.MultirangeGetter); ok {
			return multirangeGetter{source: source, m: m}
		}
	}
	return value
}
func scanContainer(codec pgtype.Codec, value any) any {
	switch codec.(type) {
	case *pgtype.ArrayCodec:
		if source, ok := value.(pgtype.ArraySetter); ok {
			return arraySetter{source: source}
		}
	case *pgtype.CompositeCodec:
		if source, ok := value.(pgtype.CompositeIndexScanner); ok {
			return compositeSetter{source: source}
		}
	case *pgtype.RangeCodec:
		if source, ok := value.(pgtype.RangeScanner); ok {
			return rangeSetter{source: source}
		}
	case *pgtype.MultirangeCodec:
		if source, ok := value.(pgtype.MultirangeSetter); ok {
			return multirangeSetter{source: source}
		}
	}
	return value
}

type arrayGetter struct {
	privateSQLValue
	source pgtype.ArrayGetter
	m      *pgtype.Map
}

func (a arrayGetter) Dimensions() []pgtype.ArrayDimension { return a.source.Dimensions() }
func (a arrayGetter) Index(i int) any                     { return protectValue(a.m, a.source.Index(i)) }
func (a arrayGetter) IndexType() any                      { return protectValue(a.m, a.source.IndexType()) }

type compositeGetter struct {
	privateSQLValue
	source pgtype.CompositeIndexGetter
	m      *pgtype.Map
}

func (a compositeGetter) IsNull() bool    { return a.source.IsNull() }
func (a compositeGetter) Index(i int) any { return protectValue(a.m, a.source.Index(i)) }

type rangeGetter struct {
	privateSQLValue
	source pgtype.RangeValuer
	m      *pgtype.Map
}

func (a rangeGetter) IsNull() bool                                     { return a.source.IsNull() }
func (a rangeGetter) BoundTypes() (pgtype.BoundType, pgtype.BoundType) { return a.source.BoundTypes() }
func (a rangeGetter) Bounds() (any, any) {
	lo, hi := a.source.Bounds()
	return protectValue(a.m, lo), protectValue(a.m, hi)
}

type multirangeGetter struct {
	privateSQLValue
	source pgtype.MultirangeGetter
	m      *pgtype.Map
}

func (a multirangeGetter) IsNull() bool    { return a.source.IsNull() }
func (a multirangeGetter) Len() int        { return a.source.Len() }
func (a multirangeGetter) Index(i int) any { return protectValue(a.m, a.source.Index(i)) }
func (a multirangeGetter) IndexType() any  { return protectValue(a.m, a.source.IndexType()) }

type arraySetter struct {
	privateSQLValue
	source pgtype.ArraySetter
}

func (a arraySetter) SetDimensions(dimensions []pgtype.ArrayDimension) error {
	return sqlcallback.Invoke("set PostgreSQL array dimensions", func() error { return a.source.SetDimensions(dimensions) })
}
func (a arraySetter) ScanIndex(i int) any { return protectTarget(a.source.ScanIndex(i)) }
func (a arraySetter) ScanIndexType() any  { return protectTarget(a.source.ScanIndexType()) }

type compositeSetter struct {
	privateSQLValue
	source pgtype.CompositeIndexScanner
}

func (a compositeSetter) ScanNull() error {
	return sqlcallback.Invoke("scan PostgreSQL composite null", a.source.ScanNull)
}
func (a compositeSetter) ScanIndex(i int) any { return protectTarget(a.source.ScanIndex(i)) }

type rangeSetter struct {
	privateSQLValue
	source pgtype.RangeScanner
}

func (a rangeSetter) ScanNull() error {
	return sqlcallback.Invoke("scan PostgreSQL range null", a.source.ScanNull)
}
func (a rangeSetter) ScanBounds() (any, any) {
	lo, hi := a.source.ScanBounds()
	return protectTarget(lo), protectTarget(hi)
}
func (a rangeSetter) SetBoundTypes(lo, hi pgtype.BoundType) error {
	return sqlcallback.Invoke("set PostgreSQL range bounds", func() error { return a.source.SetBoundTypes(lo, hi) })
}

type multirangeSetter struct {
	privateSQLValue
	source pgtype.MultirangeSetter
}

func (a multirangeSetter) ScanNull() error {
	return sqlcallback.Invoke("scan PostgreSQL multirange null", a.source.ScanNull)
}
func (a multirangeSetter) SetLen(n int) error {
	return sqlcallback.Invoke("set PostgreSQL multirange length", func() error { return a.source.SetLen(n) })
}
func (a multirangeSetter) ScanIndex(i int) any { return protectTarget(a.source.ScanIndex(i)) }
func (a multirangeSetter) ScanIndexType() any  { return protectTarget(a.source.ScanIndexType()) }

// Known native containers keep their capabilities for user-registered types that
// have no default Go-type mapping. Built-in codecs still recover the original
// value before choosing a conversion, preserving multi-interface precedence.
type protectedArray struct {
	arrayGetter
	original any
}

func (v protectedArray) originalValue() any { return v.original }

type protectedComposite struct {
	compositeGetter
	original any
}

func (v protectedComposite) originalValue() any { return v.original }

type protectedRange struct {
	rangeGetter
	original any
}

func (v protectedRange) originalValue() any { return v.original }

type protectedMultirange struct {
	multirangeGetter
	original any
}

func (v protectedMultirange) originalValue() any { return v.original }

type protectedArrayTarget struct {
	arraySetter
	original any
}

func (v protectedArrayTarget) originalTarget() any { return v.original }

type protectedCompositeTarget struct {
	compositeSetter
	original any
}

func (v protectedCompositeTarget) originalTarget() any { return v.original }

type protectedRangeTarget struct {
	rangeSetter
	original any
}

func (v protectedRangeTarget) originalTarget() any { return v.original }

type protectedMultirangeTarget struct {
	multirangeSetter
	original any
}

func (v protectedMultirangeTarget) originalTarget() any { return v.original }
