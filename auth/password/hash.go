package password

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Hash is a validated, bounded Argon2id PHC value. It is distinct from plaintext
// and redacts ordinary formatting/JSON. Encoded is the explicit persistence or
// migration boundary. Hash has no JSONContract: it is not a public response field.
// Imported Argon2i PHC and bcrypt ($2a$/$2b$/$2y$, for example from Laravel)
// hashes are accepted for verification only; NeedsRehash reports them so a
// successful login replaces them with Argon2id.
type Hash struct{ encoded secret.String }

func ParseHash(encoded secret.String) (Hash, error) {
	if _, err := parse(encoded.Reveal()); err != nil {
		return Hash{}, err
	}
	return Hash{encoded: encoded}, nil
}
func (h Hash) Validate() error            { _, err := parse(h.encoded.Reveal()); return err }
func (h Hash) Encoded() secret.String     { return h.encoded }
func (Hash) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Hash) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Hash) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

type algorithm uint8

const (
	argon2id algorithm = iota
	argon2i
	bcryptHash
)

type parsedHash struct {
	algorithm  algorithm
	parameters Parameters
	salt, key  []byte
	bcryptCost int
}

// bcryptLength is the fixed modular-crypt length: $2b$NN$ + 53 base64 bytes.
const bcryptLength = 60

func parseBcrypt(raw string) (parsedHash, error) {
	if len(raw) != bcryptLength || raw[0] != '$' || raw[1] != '2' || !strings.ContainsRune("aby", rune(raw[2])) || raw[3] != '$' || raw[6] != '$' {
		return parsedHash{}, invalidHash()
	}
	cost, err := strconv.Atoi(raw[4:6])
	if err != nil || cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return parsedHash{}, invalidHash()
	}
	for i := 7; i < len(raw); i++ {
		c := raw[i]
		if !(c == '.' || c == '/' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return parsedHash{}, invalidHash()
		}
	}
	return parsedHash{algorithm: bcryptHash, bcryptCost: cost}, nil
}

func invalidHash() error { return fault.New(fault.Invalid, "invalid or unsupported password hash") }
func canonicalNumber(raw string, bits int) (uint64, error) {
	if raw == "" || len(raw) > 10 {
		return 0, invalidHash()
	}
	value, err := strconv.ParseUint(raw, 10, bits)
	if err != nil || strconv.FormatUint(value, 10) != raw {
		return 0, invalidHash()
	}
	return value, nil
}
func decodedHashPart(raw string, minimum, maximum int) ([]byte, error) {
	if len(raw) > base64.RawStdEncoding.EncodedLen(maximum) {
		return nil, invalidHash()
	}
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) < minimum || len(decoded) > maximum || base64.RawStdEncoding.EncodeToString(decoded) != raw {
		return nil, invalidHash()
	}
	return decoded, nil
}
func parse(raw string) (parsedHash, error) {
	if len(raw) > MaxEncodedBytes {
		return parsedHash{}, invalidHash()
	}
	if strings.HasPrefix(raw, "$2") {
		return parseBcrypt(raw)
	}
	parts := strings.Split(raw, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" && parts[1] != "argon2i" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return parsedHash{}, invalidHash()
	}
	variant := argon2id
	if parts[1] == "argon2i" {
		variant = argon2i
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return parsedHash{}, invalidHash()
	}
	var values [3]uint64
	for i, prefix := range []string{"m=", "t=", "p="} {
		number, ok := strings.CutPrefix(parameters[i], prefix)
		if !ok {
			return parsedHash{}, invalidHash()
		}
		bits := 32
		if i == 2 {
			bits = 8
		}
		value, err := canonicalNumber(number, bits)
		if err != nil {
			return parsedHash{}, err
		}
		values[i] = value
	}
	cost := Parameters{MemoryKiB: uint32(values[0]), Iterations: uint32(values[1]), Parallelism: uint8(values[2])}
	if err := cost.Validate(); err != nil {
		return parsedHash{}, invalidHash()
	}
	salt, err := decodedHashPart(parts[4], 8, 64)
	if err != nil {
		return parsedHash{}, err
	}
	key, err := decodedHashPart(parts[5], 16, 64)
	if err != nil {
		return parsedHash{}, err
	}
	return parsedHash{algorithm: variant, parameters: cost, salt: salt, key: key}, nil
}
func encodedHash(parameters Parameters, salt, key []byte) Hash {
	text := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, parameters.MemoryKiB, parameters.Iterations, parameters.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	return Hash{encoded: secret.New(text)}
}
