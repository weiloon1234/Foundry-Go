package querycore

import (
	"bytes"
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"slices"
)

// Sealer encrypts one captured plaintext for the row it is written to.
type Sealer func(context.Context, *encryption.Keyring, encryption.Context) (AssignmentValue, error)

func HasEncryptedAssignments(assignments []MutationAssignment) bool {
	for _, assignment := range assignments {
		if assignment.Seal != nil {
			return true
		}
	}
	return false
}

func SealAssignments(ctx context.Context, keys *encryption.Keyring, table string, assignments []MutationAssignment, key func() (driver.Value, error)) ([]MutationAssignment, error) {
	if !HasEncryptedAssignments(assignments) {
		return assignments, nil
	}
	if keys == nil {
		return nil, fault.New(fault.Missing, "database has no encryption key ring for encrypted model fields")
	}
	row, err := key()
	if err != nil {
		return nil, err
	}
	result := slices.Clone(assignments)
	for i, assignment := range result {
		if assignment.Seal == nil {
			continue
		}
		binding, err := encrypted.Binding(table, assignment.Field.Column, row)
		if err != nil {
			return nil, err
		}
		sealed, err := assignment.Seal(ctx, keys, binding)
		if err != nil {
			return nil, err
		}
		result[i].AssignmentValue, result[i].Seal = sealed, nil
	}
	return result, nil
}

// assignedKey is an insert's primary key: its own explicit assignment.
func AssignedKey(primary string, assignments []MutationAssignment) func() (driver.Value, error) {
	return func() (driver.Value, error) {
		for _, assignment := range assignments {
			if assignment.Field.Column == primary {
				return assignment.Bind()
			}
		}
		return nil, fault.New(fault.Invalid, "encrypted fields require an assigned primary key")
	}
}

// PredicateKey is a single-row write's primary key: its one primary equality.
func PredicateKey(predicates []Expression, primary string) func() (driver.Value, error) {
	return func() (driver.Value, error) {
		var found driver.Value
		var present bool
		if err := PrimaryEqualities(predicates, primary, func(value driver.Value) error {
			if present && !SameKey(value, found) {
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

// SameKey compares bound primary keys; a []byte key is not comparable with ==.
func SameKey(left, right driver.Value) bool {
	if l, ok := left.([]byte); ok {
		r, ok := right.([]byte)
		return ok && bytes.Equal(l, r)
	}
	if _, ok := right.([]byte); ok {
		return false
	}
	return left == right
}

func PrimaryEqualities(predicates []Expression, primary string, visit func(driver.Value) error) error {
	for _, predicate := range predicates {
		switch predicate := predicate.(type) {
		case Comparison:
			field, ok := predicate.Operand.(FieldRef)
			if !ok || field.Column != primary || predicate.Operator != Equal {
				continue
			}
			if len(predicate.Values) != 1 || predicate.Bind == nil {
				return fault.New(fault.Invalid, "encrypted fields require one primary key per write")
			}
			bound, err := predicate.Bind(predicate.Values[0])
			if err != nil {
				return err
			}
			if err := visit(bound); err != nil {
				return err
			}
		case Junction:
			if !predicate.Any {
				if err := PrimaryEqualities(predicate.Children, primary, visit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func TransactionKeys(tx *database.Tx) *encryption.Keyring {
	if tx == nil {
		return nil
	}
	return tx.Encryption()
}

func OpenScope(row database.Row, table, column string, key func() (driver.Value, error)) (context.Context, *encryption.Keyring, encryption.Context, error) {
	ctx, keys, err := database.FieldEncryption(row)
	if err != nil {
		return nil, nil, encryption.Context{}, err
	}
	bound, err := key()
	if err != nil {
		return nil, nil, encryption.Context{}, err
	}
	binding, err := encrypted.Binding(table, column, bound)
	return ctx, keys, binding, err
}

func SetEncryptedError() error {
	return fault.New(fault.Invalid, "set-based writes cannot assign encrypted fields")
}
