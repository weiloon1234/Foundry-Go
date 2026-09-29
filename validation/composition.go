package validation

import (
	"github.com/weiloon1234/Foundry-Go/i18n"
	"slices"

	"github.com/weiloon1234/Foundry-Go/value"
)

// All preserves declaration order and accumulates bounded diagnostics.
func All[T any](rules ...Rule[T]) Rule[T] { return sequence(AllKind, false, rules) }

// Bail stops this sequence after its first failed rule. Other object fields
// may still be checked by an enclosing All sequence.
func Bail[T any](rules ...Rule[T]) Rule[T] { return sequence(BailKind, true, rules) }

func sequence[T any](kind Kind, bail bool, rules []Rule[T]) Rule[T] {
	if len(rules) == 0 {
		return failed[T](invalid("validation sequence requires rules"))
	}
	owned := slices.Clone(rules)
	nodes := make([]ruleNode, len(owned))
	for i, child := range owned {
		nodes[i] = child.ruleNode
	}
	return Rule[T]{ruleNode: composeNode(Description{Kind: kind}, nodes), apply: func(s *execution, input T, depth int) {
		for _, child := range owned {
			before := len(s.issues)
			child.run(s, input, depth+1)
			if s.err != nil || s.truncated || bail && len(s.issues) > before {
				return
			}
		}
	}}
}

func lift[O, I any](info Description, child Rule[I], apply func(*execution, O, int)) Rule[O] {
	return Rule[O]{ruleNode: composeNode(info, []ruleNode{child.ruleNode}), apply: apply}
}

// Field retains both the containing request and concrete field value types.
// Generated declarations own names/selectors; this is their explicit constructor.
type Field[T, V any] struct {
	name        string
	label       string
	labelKey    i18n.MessageKey
	selectValue func(T) V
	err         error
}

func DefineField[T, V any](name string, selector func(T) V) Field[T, V] {
	field := Field[T, V]{name: name, selectValue: selector}
	if !validText(name, false) || selector == nil {
		field.err = invalid("validation field requires a declared name and selector")
	}
	return field
}

func (f Field[T, V]) Validate() error {
	if f.err != nil {
		return f.err
	}
	if !validText(f.name, false) || f.selectValue == nil {
		return invalid("validation field is not defined")
	}
	return nil
}

// Name returns the generated transport property name, shared by validation and
// other typed field consumers. Validate rejects an undefined descriptor.
func (f Field[T, V]) Name() string { return f.name }

// Select reads this exact field without reflection or a second getter map.
// Like a direct application getter, it executes synchronously; services using
// custom selectors own callback isolation, cancellation and input lifetime.
func (f Field[T, V]) Select(input T) (V, error) {
	if err := f.Validate(); err != nil {
		return *new(V), err
	}
	return f.selectValue(input), nil
}

// Rules binds value rules to this field without dynamic field lookups. The
// selector runs once for this binding and must not mutate the input.
func (f Field[T, V]) Rules(rules ...Rule[V]) Rule[T] {
	if err := f.Validate(); err != nil {
		return failed[T](err)
	}
	child := All(rules...)
	return lift(Description{Kind: FieldKind, Field: f.name, Label: f.label, LabelKey: f.labelKey}, child, func(s *execution, input T, depth int) {
		selected := f.selectValue(input)
		previous, previousKey, previousField := s.label, s.labelKey, s.field
		s.label, s.labelKey, s.field = f.label, f.labelKey, f.name
		s.enter(f.name)
		defer func() { s.leave(); s.label, s.labelKey, s.field = previous, previousKey, previousField }()
		child.run(s, selected, depth+1)
	})
}

// Optional skips an omitted value and validates present values, including zero.
// It does not infer wire presence from an ordinary Go field's zero value.
func Optional[T any](rules ...Rule[T]) Rule[value.Optional[T]] {
	child := All(rules...)
	return lift(Description{Kind: OptionalKind}, child, func(s *execution, input value.Optional[T], depth int) {
		if selected, set := input.Get(); set {
			child.run(s, selected, depth+1)
		}
	})
}

// Nullable skips explicit null and validates a present value. Combine it with
// Optional for a patch that distinguishes omitted, null and replacement values.
func Nullable[T any](rules ...Rule[T]) Rule[value.Nullable[T]] {
	child := All(rules...)
	return lift(Description{Kind: NullableKind}, child, func(s *execution, input value.Nullable[T], depth int) {
		if selected, set := input.Get(); set {
			child.run(s, selected, depth+1)
		}
	})
}

// Present requires an Optional to have been supplied; its inner zero or null
// remains distinct and can be checked by additional value rules.
func Present[T any]() Rule[value.Optional[T]] {
	return valueRule(Spec{ID: "foundry.present"}, false, func(_ *execution, input value.Optional[T]) (bool, error) { return input.IsSet(), nil })
}

// NotNull rejects explicit null. Presence is a separate Optional contract.
func NotNull[T any]() Rule[value.Nullable[T]] {
	return valueRule(Spec{ID: "foundry.not_null"}, false, func(_ *execution, input value.Nullable[T]) (bool, error) { return !input.IsNull(), nil })
}

// Each checks an ordinary or named slice in input order and uses index paths.
// Empty slices have no elements; collection-size rules are separate.
func Each[S ~[]T, T any](rules ...Rule[T]) Rule[S] {
	child := All(rules...)
	return lift(Description{Kind: EachKind, ServerOnly: collectionWireTransform[S, T]()}, child, func(s *execution, input S, depth int) {
		for i, item := range input {
			s.enterIndex(i)
			child.run(s, item, depth+1)
			s.leave()
			if s.err != nil || s.truncated {
				return
			}
		}
	})
}
