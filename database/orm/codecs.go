package orm

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ColumnOption supplies a typed custom codec. Example: ColumnOf(b, &o.Code, Codec(custom)).
type ColumnOption[V any] struct {
	get     func() codec.Codec[V]
	mutate  func(V) (V, error)
	mutator bool
}

// Codec selects an explicit codec for a custom value. Example: Codec(codec.String[Code]()).
func Codec[V any](c codec.Codec[V]) ColumnOption[V] {
	return ColumnOption[V]{get: func() codec.Codec[V] { return c }}
}

// NumberValue permits exact and approximate numeric fields. Example: Number[Order, int64].
type NumberValue interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64 | decimal.Decimal
}

// TimeValue permits supported temporal fields. Example: Time[Order, temporal.DateTime].
type TimeValue interface {
	time.Time | temporal.DateTime | temporal.Date | temporal.Time | temporal.LocalDateTime
}

// InstantValue permits managed timestamp fields. Example: CreatedAtOf(b, &o.CreatedAt).
type InstantValue interface{ time.Time | temporal.DateTime }

func scalarCodec[V any]() codec.Codec[V] { return ormcore.ScalarCodec[V]() }

func selectedCodec[V any](opts []ColumnOption[V]) codec.Codec[V] {
	return optionCodec(scalarCodec[V](), opts)
}
func optionCodec[V any](fallback codec.Codec[V], opts []ColumnOption[V]) codec.Codec[V] {
	selected := false
	for _, opt := range opts {
		if opt.get != nil {
			if selected {
				return codec.Codec[V]{}
			}
			fallback = opt.get()
			selected = true
		} else if !opt.mutator {
			return codec.Codec[V]{}
		}
	}
	return fallback
}

func isInstant(t reflect.Type) bool {
	return t == reflect.TypeFor[time.Time]() || t == reflect.TypeFor[temporal.DateTime]()
}
func isNullableInstant(t reflect.Type) bool {
	return t == reflect.TypeFor[value.Nullable[time.Time]]() || t == reflect.TypeFor[value.Nullable[temporal.DateTime]]()
}
func invalid(message string) error { return fault.New(fault.Invalid, "orm: "+message) }

// IDOf declares a model-owned foreign ID column. Example: IDOf(b, &o.BuyerID).
func IDOf[M, T any](b Builder[M], ptr *model.ID[T]) Column[M, model.ID[T]] {
	return Column[M, model.ID[T]]{column: bindModel[M](b.state, ptr, codec.ID[T](), false)}
}

// NullableIDOf declares a nullable model-owned ID. Example: NullableIDOf(b, &u.IntroducerID).
func NullableIDOf[M, T any](b Builder[M], ptr *value.Nullable[model.ID[T]]) NullableColumn[M, model.ID[T]] {
	return NullableColumn[M, model.ID[T]]{nullableColumn: bindNullableModel[M](b.state, ptr, codec.ID[T]())}
}

// Key selection happens once. Exact model IDs retain the original codec's
// closures; named scalars use the same scalar codecs as ordinary columns.
func keyCodec[M any, K comparable](opts []ColumnOption[K]) (codec.Codec[K], func() error) {
	for _, opt := range opts {
		if !opt.mutator {
			return selectedCodec(opts), nil
		}
	}
	if c, ok := any(codec.ID[M]()).(codec.Codec[K]); ok {
		return c, nil
	}
	typ := reflect.TypeFor[K]()
	if typ.PkgPath() == "github.com/weiloon1234/Foundry-Go/model" && strings.HasPrefix(typ.Name(), "ID[") {
		return codec.Codec[K]{}, nil
	}
	switch typ.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return codec.Codec[K]{}, nil
	}
	c := ormcore.KeyScalarCodec[K]()
	method := reflect.ValueOf(*new(K)).MethodByName("EnumDescriptor")
	if !method.IsValid() {
		return c, nil
	}
	if method.Type().NumIn() != 0 || method.Type().NumOut() != 1 {
		return codec.Codec[K]{}, nil
	}
	resolve := sync.OnceValues(func() (map[K]bool, error) {
		descriptor := method.Call(nil)[0]
		validator, ok := descriptor.Interface().(interface{ Validate() error })
		if !ok || validator.Validate() != nil {
			return nil, invalid("invalid enum descriptor")
		}
		cases := descriptor.MethodByName("Cases")
		if !cases.IsValid() || cases.Type().NumIn() != 0 || cases.Type().NumOut() != 1 {
			return nil, invalid("invalid enum descriptor")
		}
		values := cases.Call(nil)[0]
		if values.Kind() != reflect.Slice {
			return nil, invalid("invalid enum descriptor")
		}
		allowed := make(map[K]bool, values.Len())
		for i := 0; i < values.Len(); i++ {
			item := values.Index(i)
			if item.Kind() != reflect.Struct {
				return nil, invalid("invalid enum descriptor")
			}
			field := item.FieldByName("Value")
			if !field.IsValid() || !field.CanInterface() {
				return nil, invalid("invalid enum descriptor")
			}
			v, ok := field.Interface().(K)
			if !ok {
				return nil, invalid("invalid enum descriptor")
			}
			allowed[v] = true
		}
		return allowed, nil
	})
	check := func() error { _, err := resolve(); return err }
	return c.Validated(func(v K) error {
		allowed, err := resolve()
		if err != nil {
			return err
		}
		if !allowed[v] {
			return invalid("invalid enum value")
		}
		return nil
	}), check
}

// FoundryCodec supplies an explicit codec option to the row declaration package.
// Example: FoundryCodec(option, sqlowner.Metadata()).
func FoundryCodec[V any](option ColumnOption[V], seal sqlowner.MetadataSeal) codec.Codec[V] {
	if !seal.Valid() || option.mutator {
		return codec.Codec[V]{}
	}
	return selectedCodec([]ColumnOption[V]{option})
}
