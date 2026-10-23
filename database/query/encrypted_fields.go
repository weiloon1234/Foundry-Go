package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type sealer = querycore.Sealer

// AssignEncrypted captures an encrypted field's plaintext. The write path seals
// it inside the write transaction with the database key ring, bound to the
// written row's table, column and primary key; it is never bound unsealed.
func AssignEncrypted[M any, V encrypted.Value[V]](table, column string, c codec.Codec[V], v V) Assignment[M] {
	assignment := Assign[M](table, column, c, v)
	assignment.mutationAssignment.Seal = querycore.EncryptedSealer(v, func(v V) assignmentValue { return captureAssignment(c, v) })
	return assignment
}

// AssignNullableEncrypted is AssignEncrypted for a nullable column; an
// explicit NULL is stored without encryption.
func AssignNullableEncrypted[M any, V encrypted.Value[V]](table, column string, c codec.Codec[value.Nullable[V]], v value.Nullable[V]) Assignment[M] {
	assignment := Assign[M](table, column, c, v)
	assignment.mutationAssignment.Seal = querycore.NullableEncryptedSealer(v, func(v value.Nullable[V]) assignmentValue { return captureAssignment(c, v) })
	return assignment
}

func (m Mutation[M]) encrypted() bool { return querycore.HasEncryptedAssignments(m.assignments) }

// sealMutation replaces each encrypted plaintext with its sealed envelope for
// the row identified by key. A failure stores nothing.
func sealMutation[M any](ctx context.Context, keys *encryption.Keyring, table string, mutation Mutation[M], key func() (driver.Value, error)) (Mutation[M], error) {
	assignments, err := querycore.SealAssignments(ctx, keys, table, mutation.assignments, key)
	if err != nil {
		return Mutation[M]{}, err
	}
	return Mutation[M]{assignments: assignments}, nil
}
func assignedKey[M any](primary string, mutation Mutation[M]) func() (driver.Value, error) {
	return querycore.AssignedKey(primary, mutation.assignments)
}
func predicateKey(predicates []expression, primary string) func() (driver.Value, error) {
	return querycore.PredicateKey(predicates, primary)
}
func sameKey(left, right driver.Value) bool { return querycore.SameKey(left, right) }
func primaryEqualities(predicates []expression, primary string, visit func(driver.Value) error) error {
	return querycore.PrimaryEqualities(predicates, primary, visit)
}

// sealPlan seals a single-model write after field mutators, before compilation.
func (p *mutationPlan[M]) seal(ctx context.Context, keys *encryption.Keyring) error {
	if !p.mutation.encrypted() {
		return nil
	}
	if p.setBased || p.query.definition == nil {
		return errSetEncrypted()
	}
	key := predicateKey(p.query.queryCore.Predicates, p.query.definition.modelDefinition.Primary)
	if p.kind == insertModel {
		key = assignedKey(p.query.definition.modelDefinition.Primary, p.mutation)
	}
	sealed, err := sealMutation(ctx, keys, p.query.definition.modelDefinition.Table, p.mutation, key)
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
		sealed, err := sealMutation(ctx, keys, p.query.definition.modelDefinition.Table, row, assignedKey(p.query.definition.modelDefinition.Primary, row))
		if err != nil {
			return insertPlan[M]{}, err
		}
		rows[i] = sealed
	}
	p.rows = rows
	return p, nil
}

func transactionKeys(tx *database.Tx) *encryption.Keyring { return querycore.TransactionKeys(tx) }

// OpenEncrypted decrypts a hydrated encrypted field for its row with the result
// stream's key ring. Generated decoders call it after scanning; a row that
// cannot decrypt fails the read instead of publishing ciphertext.
func OpenEncrypted[V encrypted.Value[V], K any](row database.Row, table, column string, key codec.Codec[K], id K, field *V) error {
	return querycore.OpenEncryptedValue(row, table, column, func() (driver.Value, error) { return key.Bind(id) }, field)
}

// OpenNullableEncrypted is OpenEncrypted for a nullable column; NULL stays NULL.
func OpenNullableEncrypted[V encrypted.Value[V], K any](row database.Row, table, column string, key codec.Codec[K], id K, field *value.Nullable[V]) error {
	return querycore.OpenNullableEncryptedValue(row, table, column, func() (driver.Value, error) { return key.Bind(id) }, field)
}

func openScope[K any](row database.Row, table, column string, key codec.Codec[K], id K) (context.Context, *encryption.Keyring, encryption.Context, error) {
	return querycore.OpenScope(row, table, column, func() (driver.Value, error) { return key.Bind(id) })
}

// EncryptedField names an encrypted column. Its values are randomized
// envelopes, so it offers no comparison, ordering or conflict update; writes go
// through generated drafts.
type EncryptedField[M, V any] struct{ ref fieldRef }

func NewEncryptedField[M, V any](table, column string, _ codec.Codec[V]) EncryptedField[M, V] {
	return EncryptedField[M, V]{fieldRef{Table: table, Column: column}}
}

// NullableEncryptedField adds only the NULL predicates, which do not reveal or
// compare the encrypted value.
type NullableEncryptedField[M, V any] struct {
	ref   fieldRef
	codec codec.Codec[V]
}

func NewNullableEncryptedField[M, V any](table, column string, c codec.Codec[V]) NullableEncryptedField[M, V] {
	return NullableEncryptedField[M, V]{fieldRef{Table: table, Column: column}, c}
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
	return querycore.SetEncryptedError()
}
