package orm

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Slot binds an extension value without persisting a model column.
// Example: Title orm.Slot[Article,translations.Text].
type Slot[M, S any] struct {
	_    [0]*M
	_    [0]*S
	core *slotCore
}

// SlotOf binds a concrete extension field inside Define. Example: orm.SlotOf(b,&article.Title).
func SlotOf[M, S any](b Builder[M], ptr *S) Slot[M, S] {
	return Slot[M, S]{core: bindSlot(b.state, unsafe.Pointer(ptr), reflect.TypeFor[S](), reflect.TypeFor[S]())}
}

// FoundrySlot provides typed accessors only to framework metadata adapters.
// Example: orm.FoundrySlot(Articles.Key(),Articles.Title,sqlowner.Metadata()).
func FoundrySlot[M any, K comparable, S any](key Key[M, K], slot Slot[M, S], seal sqlowner.MetadataSeal) (string, func(M) model.Reference[M, K], func(M) S, func(M, S) M, error) {
	s := slot.core
	if !seal.Valid() || s == nil || s.err != nil || key.key.core == nil || s.owner != key.key.core.owner || s.owner.primary != key.key.core {
		return "", nil, nil, nil, invalid("extension slot requires its owner's primary key and metadata capability")
	}
	if err := tableError(s.owner); err != nil {
		return "", nil, nil, nil, err
	}
	identity := ModelIdentity[M, K]{table: s.owner, key: key.key}
	get := func(m M) S { return *(*S)(unsafe.Add(unsafe.Pointer(&m), s.offset)) }
	set := func(m M, value S) M { *(*S)(unsafe.Add(unsafe.Pointer(&m), s.offset)) = value; return m }
	return s.name, identity.ReferenceOf, get, set, nil
}

// ExtensionRelation is a model-owned external-store loader. Example: Articles.Query().With(title).
type ExtensionRelation[M any] struct {
	_    [0]*M
	plan *relationPlan
}

func (r ExtensionRelation[M]) relationPlan(*M) *relationPlan { return r.plan }
func (r ExtensionRelation[M]) ormError() error               { return validateRelation(r.plan, nil) }

// FoundryExtension binds a shared external loader using a validated framework capability.
// Example: orm.FoundryExtension(Articles.Title,loader,sqlowner.Queries()).
func FoundryExtension[M, S any](slot Slot[M, S], source extensionslot.Slot[M], seal sqlowner.QuerySeal) ExtensionRelation[M] {
	if !seal.Valid() {
		return ExtensionRelation[M]{plan: &relationPlan{err: invalid("invalid extension slot bridge")}}
	}
	name, table, kind, err := source.Metadata()
	return extensionRelation(slot.core, reflect.TypeFor[S](), name, table, kind, err, source.Load)
}

func extensionRelation[M any](slot *slotCore, expected reflect.Type, name, table string, kind reflect.Type, err error, load func(context.Context, database.Executor, []M, querycore.RelationLimits, *int, *int, bool) ([]M, error)) ExtensionRelation[M] {
	p := &relationPlan{slot: slot, err: err, external: true}
	if slot == nil || slot.owner == nil || slot.name != name || kind != expected || slot.owner.model.Table != table || load == nil {
		p.err = invalid("extension loader does not match its declared slot")
		return ExtensionRelation[M]{plan: p}
	}
	p.loaded = func(unsafe.Pointer) bool { return false } // The framework loader owns loaded-empty semantics.
	p.load = func(ctx context.Context, ex database.Executor, _ *relationPlan, base unsafe.Pointer, count int, budget *relationBudget, _ int, missing bool) error {
		parents := unsafe.Slice((*M)(base), count)
		loaded, err := load(ctx, ex, parents, querycore.RelationLimits(budget.limits), &budget.remaining, &budget.attachments, missing)
		if err != nil {
			return err
		}
		copy(parents, loaded)
		return nil
	}
	return ExtensionRelation[M]{plan: p}
}
