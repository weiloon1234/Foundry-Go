// Package encrypted provides model field types that are stored as authenticated
// encryption envelopes, like Laravel's encrypted casts. Generated models encrypt
// assigned values with the database's key ring inside the write and decrypt them
// while hydrating, so application code reads and assigns plaintext. Each value is
// bound to its table, column and row primary key: a ciphertext copied to another
// row, column or table does not decrypt. Values never appear in formatting, JSON,
// logs or audit records, and encrypted columns cannot be compared or ordered in
// queries because every write uses a fresh random nonce.
package encrypted

import (
	"context"
	"crypto/subtle"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Purpose is the encryption purpose of every encrypted model field.
const Purpose encryption.Purpose = "foundry.model.field.v1"

// Value is implemented by encrypted field types. Seal encrypts an assigned
// plaintext for one record; Open decrypts a stored envelope for one record;
// Equal compares plaintexts for change detection. Generated code calls these;
// applications assign and read plaintext through each type's own methods.
type Value[V any] interface {
	Seal(context.Context, *encryption.Keyring, encryption.Context) (V, error)
	Open(context.Context, *encryption.Keyring, encryption.Context) (V, error)
	Equal(V) bool
}

// Binding is the authenticated context of one stored value: its table, column
// and row primary key, never stored in the envelope. The key is the primary
// key's bound SQL value, so an application-assigned key is required.
func Binding(table, column string, key driver.Value) (encryption.Context, error) {
	text, err := keyText(key)
	if err != nil {
		return encryption.Context{}, err
	}
	if table == "" || column == "" {
		return encryption.Context{}, fault.New(fault.Invalid, "encrypted field binding requires a table and column")
	}
	return encryption.NewContext(Purpose, secret.New(strconv.Quote(table)+":"+strconv.Quote(column)+":"+text))
}

func keyText(key driver.Value) (string, error) {
	switch v := key.(type) {
	case string:
		if v != "" {
			return "s" + strconv.Quote(v), nil
		}
	case int64:
		return "i" + strconv.FormatInt(v, 10), nil
	case []byte:
		if len(v) != 0 {
			return "b" + hex.EncodeToString(v), nil
		}
	}
	return "", fault.New(fault.Invalid, "encrypted fields require an assigned primary key")
}

// state is shared by both field types: the stored envelope and, once
// assigned or opened, the plaintext. A hydrated value holds both.
type state struct {
	sealed encryption.Ciphertext
	plain  secret.String
	open   bool
}

func (s state) seal(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (state, error) {
	if keys == nil {
		return state{}, fault.New(fault.Missing, "database has no encryption key ring for encrypted model fields")
	}
	if !s.open {
		return state{}, fault.New(fault.Invalid, "encrypted field has no plaintext to store")
	}
	sealed, err := keys.Encrypt(ctx, binding, s.plain)
	if err != nil {
		return state{}, err
	}
	return state{sealed: sealed, plain: s.plain, open: true}, nil
}

func (s state) unseal(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (state, error) {
	if keys == nil {
		return state{}, fault.New(fault.Missing, "database has no encryption key ring for encrypted model fields")
	}
	if s.sealed.IsZero() {
		return state{}, fault.New(fault.Invalid, "encrypted field has no stored value")
	}
	plain, err := keys.Decrypt(ctx, binding, s.sealed)
	if err != nil {
		return state{}, err
	}
	return state{sealed: s.sealed, plain: plain, open: true}, nil
}

func (s state) equal(other state) bool {
	if s.open && other.open {
		return subtle.ConstantTimeCompare([]byte(s.plain.Reveal()), []byte(other.plain.Reveal())) == 1
	}
	return !s.sealed.IsZero() && s.sealed == other.sealed
}

func (s state) bind() (driver.Value, error) {
	if s.sealed.IsZero() {
		return nil, fault.New(fault.Invalid, "encrypted field was not sealed before binding")
	}
	return s.sealed.Encoded(), nil
}

func decodeState(source any) (state, error) {
	text, err := codec.String[string]().Decode(source)
	if err != nil {
		return state{}, err
	}
	sealed, err := encryption.ParseCiphertext(text)
	if err != nil {
		return state{}, err
	}
	return state{sealed: sealed}, nil
}

// Text is an encrypted string field (Laravel's encrypted cast). Assign NewText
// and read Reveal. The zero value has no plaintext and cannot be stored.
type Text struct{ state state }

func NewText(plain string) Text {
	return Text{state{plain: secret.New(plain), open: true}}
}

// Reveal returns the plaintext of an assigned or hydrated value; a value that
// was never opened reveals nothing.
func (t Text) Reveal() string { return t.state.plain.Reveal() }

// IsZero reports a value with neither plaintext nor a stored envelope.
func (t Text) IsZero() bool { return !t.state.open && t.state.sealed.IsZero() }

func (t Text) Seal(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (Text, error) {
	sealed, err := t.state.seal(ctx, keys, binding)
	return Text{sealed}, err
}
func (t Text) Open(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (Text, error) {
	opened, err := t.state.unseal(ctx, keys, binding)
	return Text{opened}, err
}
func (t Text) Equal(other Text) bool      { return t.state.equal(other.state) }
func (Text) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Text) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Text) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(secret.Redacted)), nil }
func (Text) MarshalText() ([]byte, error) { return []byte(secret.Redacted), nil }
func (t Text) GoString() string           { return secret.Redacted }

// TextCodec stores the envelope as text. Binding requires a sealed value, so
// only the framework's write path can store it; hydration yields a sealed value
// that the generated decoder opens. Values are sensitive and compare by plaintext.
func TextCodec() codec.Codec[Text] {
	return codec.New(func(t Text) (driver.Value, error) { return t.state.bind() }, func(source any) (Text, error) {
		decoded, err := decodeState(source)
		return Text{decoded}, err
	}).WithParameterType(codec.TypeText).WithSensitiveValues().WithEquality(Text.Equal)
}

// JSON is an encrypted typed JSON field (Laravel's encrypted:array and
// encrypted:json casts). The plaintext is the canonical JSON of T.
type JSON[T any] struct {
	state state
}

// NewJSON snapshots input through value.NewJSON before it is stored.
func NewJSON[T any](input T) (JSON[T], error) {
	snapshot, err := value.NewJSON(input)
	if err != nil {
		return JSON[T]{}, err
	}
	text, err := snapshot.Text()
	if err != nil {
		return JSON[T]{}, err
	}
	return JSON[T]{state{plain: secret.New(text), open: true}}, nil
}

// Decode returns a fresh T from the plaintext JSON with value.ParseJSON's
// strict shape checks.
func (j JSON[T]) Decode() (T, error) {
	if !j.state.open {
		return *new(T), fault.New(fault.Invalid, "encrypted JSON field was not opened")
	}
	snapshot, err := value.ParseJSON[T](j.state.plain.Reveal())
	if err != nil {
		return *new(T), err
	}
	return snapshot.Decode()
}

func (j JSON[T]) IsZero() bool { return !j.state.open && j.state.sealed.IsZero() }
func (j JSON[T]) Seal(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (JSON[T], error) {
	sealed, err := j.state.seal(ctx, keys, binding)
	return JSON[T]{sealed}, err
}
func (j JSON[T]) Open(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (JSON[T], error) {
	opened, err := j.state.unseal(ctx, keys, binding)
	if err == nil {
		// Reject stored plaintext that no longer fits T before publishing it.
		if _, err = value.ParseJSON[T](opened.plain.Reveal()); err != nil {
			return JSON[T]{}, err
		}
	}
	return JSON[T]{opened}, err
}
func (j JSON[T]) Equal(other JSON[T]) bool   { return j.state.equal(other.state) }
func (JSON[T]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (JSON[T]) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (JSON[T]) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(secret.Redacted)), nil }
func (JSON[T]) MarshalText() ([]byte, error) { return []byte(secret.Redacted), nil }
func (j JSON[T]) GoString() string           { return secret.Redacted }

// JSONCodec stores the envelope of a typed JSON plaintext as text.
func JSONCodec[T any]() codec.Codec[JSON[T]] {
	return codec.New(func(j JSON[T]) (driver.Value, error) { return j.state.bind() }, func(source any) (JSON[T], error) {
		decoded, err := decodeState(source)
		return JSON[T]{decoded}, err
	}).WithParameterType(codec.TypeText).WithSensitiveValues().WithEquality(JSON[T].Equal)
}
