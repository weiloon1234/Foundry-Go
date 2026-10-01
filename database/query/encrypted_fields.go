package query

import (
	"bytes"
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// sealer encrypts one captured plaintext for the row it is written to.
type sealer func(context.Context, *encryption.Keyring, encryption.Context) (assignmentValue, error)

// AssignEncrypted captures an encrypted field's plaintext. The write path seals
// it inside the write transaction with the database key ring, bound to the
// written row's table, column and primary key; it is never bound unsealed.
func AssignEncrypted[M any, V encrypted.Value[V]](table, column string, c codec.Codec[V], v V) Assignment[M] {
	assignment := Assign[M](table, column, c, v)
	assignment.seal = func(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (assignmentValue, error) {
		sealed, err := v.Seal(ctx, keys, binding)
		if err != nil {
			return assignmentValue{}, err
		}
		return captureAssignment(c, sealed), nil
	}
	return assignment
}

// AssignNullableEncrypted is AssignEncrypted for a nullable column; an
// explicit NULL is stored without encryption.
func AssignNullableEncrypted[M any, V encrypted.Value[V]](table, column string, c codec.Codec[value.Nullable[V]], v value.Nullable[V]) Assignment[M] {
	assignment := Assign[M](table, column, c, v)
	assignment.seal = func(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (assignmentValue, error) {
		item, present := v.Get()
		if !present {
			return captureAssignment(c, v), nil
		}
		sealed, err := item.Seal(ctx, keys, binding)
		if err != nil {
			return assignmentValue{}, err
		}
		return captureAssignment(c, value.Of(sealed)), nil
	}
	return assignment
}

func (m Mutation[M]) encrypted() bool {
	for _, assignment := range m.assignments {
		if assignment.seal != nil {
			return true
		}
	}
	return false
}

// sealMutation replaces each encrypted plaintext with its sealed envelope for
// the row identified by key. A failure stores nothing.
func sealMutation[M any](ctx context.Context, keys *encryption.Keyring, table string, mutation Mutation[M], key func() (driver.Value, error)) (Mutation[M], error) {
	if !mutation.encrypted() {
		return mutation, nil
	}
	if keys == nil {
		return Mutation[M]{}, fault.New(fault.Missing, "database has no encryption key ring for encrypted model fields")
	}
	row, err := key()
	if err != nil {
		return Mutation[M]{}, err
	}
	result := Change(mutation.assignments...)
	for i, assignment := range result.assignments {
		if assignment.seal == nil {
			continue
		}
		binding, err := encrypted.Binding(table, assignment.field.column, row)
		if err != nil {
			return Mutation[M]{}, err
		}
		sealed, err := assignment.seal(ctx, keys, binding)
		if err != nil {
			return Mutation[M]{}, err
		}
		result.assignments[i].assignmentValue, result.assignments[i].seal = sealed, nil
	}
	return result, nil
}

// assignedKey is an insert's primary key: its own explicit assignment.
func assignedKey[M any](primary string, mutation Mutation[M]) func() (driver.Value, error) {
	return func() (driver.Value, error) {
		for _, assignment := range mutation.assignments {
			if assignment.field.column == primary {
				return assignment.bind()
			}
		}
		return nil, fault.New(fault.Invalid, "encrypted fields require an assigned primary key")
	}
}

// predicateKey is a single-row write's primary key: its one primary equality.
func predicateKey(predicates []expression, primary string) func() (driver.Value, error) {
	return func() (driver.Value, error) {
		var found driver.Value
		var present bool
		if err := primaryEqualities(predicates, primary, func(value driver.Value) error {
			if present && !sameKey(value, found) {
				return fault.New(fault.Invalid, "encrypted fields require one primary key per write")
			}
			found, present = value, true
			return nil
		}); err != nil {
			return nil, err
		}
		if !present {
			return nil, fault.New(fault.Invalid, "encrypted fields require a primary-key write")
		}
		return found, nil
	}
}

// sameKey compares bound primary keys; a []byte key is not comparable with ==.
func sameKey(left, right driver.Value) bool {
	if l, ok := left.([]byte); ok {
		r, ok := right.([]byte)
		return ok && bytes.Equal(l, r)
	}
	if _, ok := right.([]byte); ok {
		return false
	}
	return left == right
}

func primaryEqualities(predicates []expression, primary string, visit func(driver.Value) error) error {
	for _, predicate := range predicates {
		switch predicate := predicate.(type) {
		case comparison:
			field, ok := predicate.operand.(fieldRef)
			if !ok || field.column != primary || predicate.operator != equal {
				continue
			}
			if len(predicate.values) != 1 || predicate.bind == nil {
				return fault.New(fault.Invalid, "encrypted fields require one primary key per write")
			}
			bound, err := predicate.bind(predicate.values[0])
			if err != nil {
				return err
			}
			if err := visit(bound); err != nil {
				return err
			}
		case junction:
			if !predicate.any {
				if err := primaryEqualities(predicate.children, primary, visit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// sealPlan seals a single-model write after field mutators, before compilation.
func (p *mutationPlan[M]) seal(ctx context.Context, keys *encryption.Keyring) error {
	if !p.mutation.encrypted() {
		return nil
	}
	if p.setBased || p.query.definition == nil {
		return errSetEncrypted()
	}
	key := predicateKey(p.query.predicates, p.query.definition.primary)
	if p.kind == insertModel {
		key = assignedKey(p.query.definition.primary, p.mutation)
	}
	sealed, err := sealMutation(ctx, keys, p.query.definition.table, p.mutation, key)
	if err != nil {
		return err
	}
	p.mutation = sealed
	return nil
}

func (p insertPlan[M]) encrypted() bool {
	for _, row := range p.rows {
		if row.encrypted() {
			return true
		}
	}
	return false
}

// sealRows seals each inserted row for its own assigned primary key.
func (p insertPlan[M]) sealRows(ctx context.Context, keys *encryption.Keyring) (insertPlan[M], error) {
	if !p.encrypted() {
		return p, nil
	}
	if p.query.definition == nil {
		return insertPlan[M]{}, fault.New(fault.Invalid, "encrypted fields require model metadata")
	}
	rows := make([]Mutation[M], len(p.rows))
	for i, row := range p.rows {
		sealed, err := sealMutation(ctx, keys, p.query.definition.table, row, assignedKey(p.query.definition.primary, row))
		if err != nil {
			return insertPlan[M]{}, err
		}
		rows[i] = sealed
	}
	p.rows = rows
	return p, nil
}

func transactionKeys(tx *database.Tx) *encryption.Keyring {
	if tx == nil {
		return nil
	}
	return tx.Encryption()
}

// OpenEncrypted decrypts a hydrated encrypted field for its row with the result
// stream's key ring. Generated decoders call it after scanning; a row that
// cannot decrypt fails the read instead of publishing ciphertext.
func OpenEncrypted[V encrypted.Value[V], K any](row database.Row, table, column string, key codec.Codec[K], id K, field *V) error {
	if field == nil {
		return fault.New(fault.Invalid, "encrypted field destination is missing")
	}
	ctx, keys, binding, err := openScope(row, table, column, key, id)
	if err != nil {
		return err
	}
	opened, err := (*field).Open(ctx, keys, binding)
	if err != nil {
		return err
	}
	*field = opened
	return nil
}

// OpenNullableEncrypted is OpenEncrypted for a nullable column; NULL stays NULL.
func OpenNullableEncrypted[V encrypted.Value[V], K any](row database.Row, table, column string, key codec.Codec[K], id K, field *value.Nullable[V]) error {
	if field == nil {
		return fault.New(fault.Invalid, "encrypted field destination is missing")
	}
	item, present := field.Get()
	if !present {
		return nil
	}
	if err := OpenEncrypted(row, table, column, key, id, &item); err != nil {
		return err
	}
	*field = value.Of(item)
	return nil
}

func openScope[K any](row database.Row, table, column string, key codec.Codec[K], id K) (context.Context, *encryption.Keyring, encryption.Context, error) {
	ctx, keys, err := database.FieldEncryption(row)
	if err != nil {
		return nil, nil, encryption.Context{}, err
	}
	bound, err := key.Bind(id)
	if err != nil {
		return nil, nil, encryption.Context{}, err
	}
	binding, err := encrypted.Binding(table, column, bound)
	return ctx, keys, binding, err
}

// EncryptedField names an encrypted column. Its values are randomized
// envelopes, so it offers no comparison, ordering or conflict update; writes go
// through generated drafts.
type EncryptedField[M, V any] struct{ ref fieldRef }

func NewEncryptedField[M, V any](table, column string, _ codec.Codec[V]) EncryptedField[M, V] {
	return EncryptedField[M, V]{fieldRef{table, column}}
}

// NullableEncryptedField adds only the NULL predicates, which do not reveal or
// compare the encrypted value.
type NullableEncryptedField[M, V any] struct {
	ref   fieldRef
	codec codec.Codec[V]
}

func NewNullableEncryptedField[M, V any](table, column string, c codec.Codec[V]) NullableEncryptedField[M, V] {
	return NullableEncryptedField[M, V]{fieldRef{table, column}, c}
}
func (f NullableEncryptedField[M, V]) IsNull() Predicate[M] {
	return Predicate[M]{expression: typedComparison(f.ref, isNull, f.codec, nil)}
}
func (f NullableEncryptedField[M, V]) IsNotNull() Predicate[M] {
	return Predicate[M]{expression: typedComparison(f.ref, isNotNull, f.codec, nil)}
}

// forwardEncryption lets framework row adapters keep the decryption scope of
// the stream they wrap; a stream without one stays unable to decrypt.
func forwardEncryption(row database.Row) (context.Context, *encryption.Keyring) {
	if scoped, ok := row.(database.EncryptedRow); ok {
		return scoped.FieldEncryption()
	}
	return nil, nil
}

// errSetEncrypted rejects one literal written to many rows: each encrypted
// value is bound to exactly one row's primary key.
func errSetEncrypted() error {
	return fault.New(fault.Invalid, "set-based writes cannot assign encrypted fields")
}
