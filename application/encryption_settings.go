package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// EncryptionKeySettings is one retired key retained to decrypt and re-encrypt
// existing data. Key is 32 bytes encoded as unpadded base64url.
//
//foundry:config
type EncryptionKeySettings struct {
	Key secret.String
}

// EncryptionKeys maps retired key IDs to their material.
type EncryptionKeys map[encryption.KeyID]EncryptionKeySettings

func (m *EncryptionKeys) UnmarshalText(data []byte) error {
	schema, err := EncryptionKeySettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(encryption.KeyID) EncryptionKeySettings { return EncryptionKeySettings{} }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

// EncryptionSettings configures the application key ring. KeyID names the
// active key and Key holds its material (32 bytes, unpadded base64url, for
// example from encryption.GenerateKey). Previous retains retired keys so
// existing data stays readable until it is re-encrypted. Load Key and previous
// keys from secret files or environment (NAME_FILE); never commit them. An
// empty KeyID disables application encryption.
type EncryptionSettings struct {
	KeyID    encryption.KeyID
	Key      secret.String
	Previous EncryptionKeys `config:",secret"`
}

// Enabled reports whether an active key is configured.
func (s EncryptionSettings) Enabled() bool { return s.KeyID != "" }

// keyring validates and parses the configured keys. It performs no I/O.
func (s EncryptionSettings) keyring() (*encryption.Keyring, error) {
	if !s.Enabled() {
		if s.Key.IsZero() && len(s.Previous) == 0 {
			return nil, nil
		}
		return nil, fault.New(fault.Invalid, "application encryption keys require an active key ID")
	}
	if len(s.Previous) >= encryption.MaxKeys {
		return nil, fault.New(fault.Invalid, "too many retained encryption keys")
	}
	active, err := encryption.ParseKey(s.KeyID, s.Key)
	if err != nil {
		return nil, err
	}
	keys := []encryption.Key{active}
	for id, retained := range s.Previous {
		if id == s.KeyID {
			return nil, fault.New(fault.Duplicate, "the active encryption key is also listed as a previous key")
		}
		key, err := encryption.ParseKey(id, retained.Key)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return encryption.NewKeyring(s.KeyID, keys...)
}

const EncryptionProvider foundation.ProviderID = "foundry.application.encryption"

// EncryptionKey resolves the application key ring when Encryption.KeyID is set.
var EncryptionKey = foundation.NewKey[*encryption.Keyring](string(EncryptionProvider))

// registerEncryption parses the keys before any resource is acquired and
// provides one immutable keyring for the application.
func registerEncryption(builder *foundation.Builder, s EncryptionSettings) error {
	keyring, err := s.keyring()
	if err != nil || keyring == nil {
		return err
	}
	builder.Register(foundation.Module{Name: EncryptionProvider, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, EncryptionKey, keyring)
	}})
	return nil
}

// Encryption returns the application key ring: new data uses the active key and
// retained previous keys decrypt existing data. It fails with fault.Missing
// when Encryption.KeyID is not configured.
func (s Services) Encryption() (*encryption.Keyring, error) {
	keyring, err := Resolve(s, EncryptionKey)
	if err != nil {
		return nil, fault.Wrap(fault.Missing, "application encryption keys are not configured", err)
	}
	return keyring, nil
}

// CookieEncrypter encrypts cookies with the application key ring and clock, for
// Cookie.Encrypted. Rotating the key keeps existing cookies readable while the
// previous key is retained.
func (s Services) CookieEncrypter() (http.CookieEncrypter, error) {
	keyring, err := s.Encryption()
	if err != nil {
		return http.CookieEncrypter{}, err
	}
	source := s.clock
	if source == nil {
		source = clock.System{}
	}
	return http.NewCookieEncrypter(keyring, source)
}
